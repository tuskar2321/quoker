package upload_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"

	"github.com/tuskar2321/quoker/internal/shared/errs"
	"github.com/tuskar2321/quoker/internal/worker/upload"
)

// fakeCore stores uploads by path and can drop a PUT halfway once.
type fakeCore struct {
	mu        sync.Mutex
	files     map[string][]byte
	cutAfter  int // when > 0, the first PUT keeps only this many bytes and fails
	ranges    []string
	thumbType string
	status    int // forced response status for every request when non-zero
}

func (f *fakeCore) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.status != 0 {
		w.WriteHeader(f.status)
		return
	}
	cur, ok := f.files[r.URL.Path]
	switch r.Method {
	case http.MethodHead:
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(cur)))
	case http.MethodPut:
		f.ranges = append(f.ranges, r.Header.Get("Content-Range"))
		if r.URL.Path == "/internal/v1/thumbs/tiktok:1" {
			f.thumbType = r.Header.Get("Content-Type")
		}
		var from int
		if cr := r.Header.Get("Content-Range"); cr != "" {
			_, _ = fmt.Sscanf(cr, "bytes %d-", &from)
			if from != len(cur) {
				w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
				return
			}
		}
		b, _ := io.ReadAll(r.Body)
		if f.cutAfter > 0 {
			f.files[r.URL.Path] = append(cur, b[:f.cutAfter]...)
			f.cutAfter = 0
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		f.files[r.URL.Path] = append(cur, b...)
	}
}

func setup(t *testing.T, core *fakeCore) (*upload.Client, string) {
	t.Helper()
	core.files = map[string][]byte{}
	srv := httptest.NewServer(core)
	t.Cleanup(srv.Close)
	return upload.New(srv.Client()).WithRetryPause(0), srv.URL + "/internal/v1"
}

func payload(t *testing.T, n int) (string, []byte) {
	t.Helper()
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i)
	}
	p := filepath.Join(t.TempDir(), "v.mp4")
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return p, b
}

func TestPutFile_resumesAfterFailure(t *testing.T) {
	core := &fakeCore{cutAfter: 300}
	c, base := setup(t, core)
	path, want := payload(t, 1000)

	var last int64
	if err := c.PutFile(context.Background(), base, "tiktok:1", path, func(s, _ int64) { last = s }); err != nil {
		t.Fatal(err)
	}
	if got := core.files["/internal/v1/files/tiktok:1"]; !bytes.Equal(got, want) {
		t.Fatalf("stored %d bytes, want %d identical", len(got), len(want))
	}
	if len(core.ranges) != 2 || core.ranges[1] != "bytes 300-999/1000" {
		t.Fatalf("ranges = %v", core.ranges)
	}
	if last != 1000 {
		t.Fatalf("progress ended at %d", last)
	}
}

func TestPutFile_alreadyComplete(t *testing.T) {
	core := &fakeCore{}
	c, base := setup(t, core)
	path, b := payload(t, 10)
	core.files["/internal/v1/files/tiktok:1"] = b
	if err := c.PutFile(context.Background(), base, "tiktok:1", path, nil); err != nil {
		t.Fatal(err)
	}
	if len(core.ranges) != 0 {
		t.Fatalf("unexpected PUTs: %v", core.ranges)
	}
}

func TestPutFile_errors(t *testing.T) {
	tests := []struct {
		status int
		want   errs.Code
	}{
		{http.StatusServiceUnavailable, errs.UpstreamError},
		{http.StatusForbidden, errs.Internal},
	}
	for _, tt := range tests {
		t.Run(strconv.Itoa(tt.status), func(t *testing.T) {
			c, base := setup(t, &fakeCore{status: tt.status})
			path, _ := payload(t, 10)
			if err := c.PutFile(context.Background(), base, "tiktok:1", path, nil); !errs.Is(err, tt.want) {
				t.Fatalf("err = %v, want %s", err, tt.want)
			}
		})
	}
}

func TestPutThumb(t *testing.T) {
	core := &fakeCore{}
	c, base := setup(t, core)
	if err := c.PutThumb(context.Background(), base, "tiktok:1", []byte("jpeg")); err != nil {
		t.Fatal(err)
	}
	if string(core.files["/internal/v1/thumbs/tiktok:1"]) != "jpeg" || core.thumbType != "image/jpeg" {
		t.Fatalf("files = %v type = %q", core.files, core.thumbType)
	}
}
