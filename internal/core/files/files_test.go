package files_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tuskar2321/quoker/internal/core/files"
	"github.com/tuskar2321/quoker/internal/core/store"
	"github.com/tuskar2321/quoker/internal/shared/errs"
	"github.com/tuskar2321/quoker/internal/worker/upload"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

type env struct {
	st   *store.Store
	fs   *files.Store
	root string
	now  time.Time
}

func newEnv(t *testing.T, maxBytes int64) *env {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "q.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	e := &env{st: st, root: t.TempDir(), now: time.Now()}
	e.fs, err = files.New(st, e.root, maxBytes, func() time.Time { return e.now }, quiet)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func (e *env) addVideo(t *testing.T, id string, lastAccess, expires time.Time) {
	t.Helper()
	_, err := e.st.DB().ExecContext(context.Background(),
		`INSERT INTO videos (id, platform, native_id, canonical_url, file_status, resolved_at, expires_at, last_access_at)
		 VALUES (?, 'tiktok', ?, 'https://x', 'none', ?, ?, ?)`,
		id, strings.TrimPrefix(id, "tiktok:"), lastAccess.UnixMilli(), expires.UnixMilli(), lastAccess.UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
}

func (e *env) store(t *testing.T, id string, size int) {
	t.Helper()
	if _, err := e.fs.Append(id, 0, int64(size), bytes.NewReader(make([]byte, size))); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.DB().ExecContext(context.Background(),
		`UPDATE videos SET file_status = 'ready', file_size = ? WHERE id = ?`, size, id); err != nil {
		t.Fatal(err)
	}
}

func (e *env) fileStatus(t *testing.T, id string) string {
	t.Helper()
	var s string
	if err := e.st.DB().QueryRowContext(context.Background(), `SELECT file_status FROM videos WHERE id = ?`, id).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestAppend(t *testing.T) {
	e := newEnv(t, 1<<20)
	data := []byte("0123456789")

	done, err := e.fs.Append("tiktok:1", 0, 10, bytes.NewReader(data[:4]))
	if err != nil || done {
		t.Fatalf("first chunk: done=%v err=%v", done, err)
	}
	if n, ok := e.fs.Received("tiktok:1"); !ok || n != 4 {
		t.Fatalf("Received = %d %v", n, ok)
	}
	if _, err := e.fs.Append("tiktok:1", 2, 10, bytes.NewReader(data[2:])); !errors.Is(err, files.ErrOffset) {
		t.Fatalf("overlapping chunk err = %v", err)
	}
	if done, err = e.fs.Append("tiktok:1", 4, 10, bytes.NewReader(data[4:])); err != nil || !done {
		t.Fatalf("last chunk: done=%v err=%v", done, err)
	}
	if !e.fs.HasFile("tiktok:1") {
		t.Fatal("file not published")
	}
	sum := sha256.Sum256(data)
	if err := e.fs.Verify("tiktok:1", 10, hex.EncodeToString(sum[:])); err != nil {
		t.Fatal(err)
	}
	if err := e.fs.Verify("tiktok:1", 10, strings.Repeat("0", 64)); err == nil || e.fs.HasFile("tiktok:1") {
		t.Fatalf("mismatch: err=%v, file kept=%v", err, e.fs.HasFile("tiktok:1"))
	}
}

func TestAppend_limits(t *testing.T) {
	e := newEnv(t, 100)
	if _, err := e.fs.Append("tiktok:1", 0, 101, strings.NewReader("x")); !errs.Is(err, errs.TooLarge) {
		t.Fatalf("err = %v, want too_large", err)
	}
	if _, err := e.fs.Append("tiktok:1", 5, 5, strings.NewReader("x")); !errs.Is(err, errs.InvalidRequest) {
		t.Fatalf("err = %v, want invalid_request", err)
	}
}

func TestPutThumb(t *testing.T) {
	e := newEnv(t, 1<<20)
	e.addVideo(t, "tiktok:1", e.now, e.now.Add(time.Hour))
	ctx := context.Background()
	if err := e.fs.PutThumb(ctx, "tiktok:1", strings.NewReader("GIF89a")); !errs.Is(err, errs.InvalidRequest) {
		t.Fatalf("non-jpeg err = %v", err)
	}
	big := append([]byte{0xFF, 0xD8, 0xFF}, make([]byte, files.MaxThumbBytes)...)
	if err := e.fs.PutThumb(ctx, "tiktok:1", bytes.NewReader(big)); !errs.Is(err, errs.TooLarge) {
		t.Fatalf("big err = %v", err)
	}
	if err := e.fs.PutThumb(ctx, "tiktok:1", bytes.NewReader([]byte{0xFF, 0xD8, 0xFF, 0xE0})); err != nil {
		t.Fatal(err)
	}
	var has int
	_ = e.st.DB().QueryRow(`SELECT has_thumbnail FROM videos WHERE id = 'tiktok:1'`).Scan(&has)
	if has != 1 {
		t.Fatal("has_thumbnail not set")
	}
}

// TestUploadContract runs the worker's upload client against the core's
// internal handler, with the connection cut halfway through the file.
func TestUploadContract(t *testing.T) {
	e := newEnv(t, 1<<30)
	verify := func(_ context.Context, fp string) (string, error) {
		if fp != "fp-ok" {
			return "", errors.New("unknown")
		}
		return "worker-1", nil
	}
	h := e.fs.InternalHandler(verify, quiet)
	var cut atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set(files.HeaderFingerprint, "fp-ok") // what Caddy adds
		if r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/files/") && cut.CompareAndSwap(false, true) {
			r.Body = io.NopCloser(&cutReader{r: r.Body, left: 3000})
		}
		h.ServeHTTP(w, r)
	}))
	defer srv.Close()

	data := make([]byte, 10_000)
	for i := range data {
		data[i] = byte(i % 251)
	}
	path := filepath.Join(t.TempDir(), "v.mp4")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	c := upload.New(srv.Client()).WithRetryPause(0)
	base := srv.URL + "/internal/v1"
	if err := c.PutFile(context.Background(), base, "tiktok:42", path, nil); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	if err := e.fs.Verify("tiktok:42", int64(len(data)), hex.EncodeToString(sum[:])); err != nil {
		t.Fatalf("stored file differs: %v", err)
	}
	if err := c.PutThumb(context.Background(), base, "tiktok:42", []byte{0xFF, 0xD8, 0xFF, 1}); err != nil {
		t.Fatal(err)
	}
}

func TestInternalHandler_rejectsUnknownWorker(t *testing.T) {
	e := newEnv(t, 1<<20)
	h := e.fs.InternalHandler(func(context.Context, string) (string, error) { return "", errors.New("unknown") }, quiet)
	for _, fp := range []string{"", "fp-bad"} {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodPut, "/internal/v1/thumbs/tiktok:1", strings.NewReader("x"))
		if fp != "" {
			req.Header.Set(files.HeaderFingerprint, fp)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("fingerprint %q: status %d", fp, rec.Code)
		}
	}
	ok := e.fs.InternalHandler(func(context.Context, string) (string, error) { return "w", nil }, quiet)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodHead, "/internal/v1/files/..%2Fetc", nil)
	req.Header.Set(files.HeaderFingerprint, "fp")
	rec := httptest.NewRecorder()
	ok.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad id: status %d", rec.Code)
	}
}

func TestCleanup(t *testing.T) {
	e := newEnv(t, 1<<30)
	ctx := context.Background()
	e.addVideo(t, "tiktok:old", e.now.Add(-48*time.Hour), e.now.Add(-time.Hour))
	e.addVideo(t, "tiktok:live", e.now, e.now.Add(time.Hour))
	e.store(t, "tiktok:old", 10)
	e.store(t, "tiktok:live", 10)
	_ = e.fs.PutThumb(ctx, "tiktok:old", bytes.NewReader([]byte{0xFF, 0xD8, 0xFF}))

	// A cover for a video whose resolve result has not arrived yet, and a
	// forgotten partial upload.
	_ = e.fs.PutThumb(ctx, "tiktok:pending", bytes.NewReader([]byte{0xFF, 0xD8, 0xFF}))
	_, _ = e.fs.Append("tiktok:stale", 0, 100, strings.NewReader("abc"))
	staleAt := e.now.Add(-2 * time.Hour)
	_ = os.Chtimes(filepath.Join(e.root, "partial", partialName(t, e)), staleAt, staleAt)

	if err := e.fs.Cleanup(ctx); err != nil {
		t.Fatal(err)
	}
	var n int
	_ = e.st.DB().QueryRow(`SELECT COUNT(*) FROM videos WHERE id = 'tiktok:old'`).Scan(&n)
	if n != 0 || e.fs.HasFile("tiktok:old") {
		t.Fatal("expired video or its file survived")
	}
	if !e.fs.HasFile("tiktok:live") {
		t.Fatal("live file removed")
	}
	if _, err := e.fs.OpenThumb("tiktok:pending"); err != nil {
		t.Fatal("fresh orphan cover removed before its grace period")
	}
	if _, ok := e.fs.Received("tiktok:stale"); ok {
		t.Fatal("stale partial upload kept")
	}

	// An hour later the cover is still an orphan and goes away.
	e.now = e.now.Add(2 * time.Hour)
	if err := e.fs.Cleanup(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := e.fs.OpenThumb("tiktok:pending"); err == nil {
		t.Fatal("orphan cover kept")
	}
}

func TestEvict(t *testing.T) {
	e := newEnv(t, 100)
	ctx := context.Background()
	for i, id := range []string{"tiktok:a", "tiktok:b", "tiktok:c"} {
		e.addVideo(t, id, e.now.Add(time.Duration(i)*time.Minute), e.now.Add(time.Hour))
		e.store(t, id, 40)
	}
	// a is the least recently used but is being downloaded right now.
	f, release, err := e.fs.OpenFile("tiktok:a")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	if err := e.fs.Evict(ctx); err != nil {
		t.Fatal(err)
	}
	if !e.fs.HasFile("tiktok:a") || e.fs.HasFile("tiktok:b") || !e.fs.HasFile("tiktok:c") {
		t.Fatalf("after evict: a=%v b=%v c=%v", e.fs.HasFile("tiktok:a"), e.fs.HasFile("tiktok:b"), e.fs.HasFile("tiktok:c"))
	}
	if got := e.fileStatus(t, "tiktok:b"); got != "none" {
		t.Fatalf("evicted file_status = %q", got)
	}
	release()
	e.addVideo(t, "tiktok:d", e.now.Add(time.Hour), e.now.Add(time.Hour))
	e.store(t, "tiktok:d", 40) // over the limit again
	if err := e.fs.Evict(ctx); err != nil {
		t.Fatal(err)
	}
	if e.fs.HasFile("tiktok:a") {
		t.Fatal("released file not evicted")
	}
}

// partialName returns the only partial upload's file name.
func partialName(t *testing.T, e *env) string {
	t.Helper()
	entries, _ := os.ReadDir(filepath.Join(e.root, "partial"))
	if len(entries) != 1 {
		t.Fatalf("partial entries = %d", len(entries))
	}
	return entries[0].Name()
}

// cutReader ends the body early, as a dropped connection would.
type cutReader struct {
	r    io.Reader
	left int
}

func (c *cutReader) Read(p []byte) (int, error) {
	if c.left <= 0 {
		return 0, io.ErrUnexpectedEOF
	}
	if len(p) > c.left {
		p = p[:c.left]
	}
	n, err := c.r.Read(p)
	c.left -= n
	return n, err
}
