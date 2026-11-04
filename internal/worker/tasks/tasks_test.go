package tasks_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	linkv1 "github.com/tuskar2321/quoker/gen/quoker/link/v1"
	"github.com/tuskar2321/quoker/internal/shared/errs"
	"github.com/tuskar2321/quoker/internal/worker/runner"
	"github.com/tuskar2321/quoker/internal/worker/tasks"
	"github.com/tuskar2321/quoker/internal/worker/telegram"
	"github.com/tuskar2321/quoker/internal/worker/tmpcache"
	"github.com/tuskar2321/quoker/internal/worker/upload"
	"github.com/tuskar2321/quoker/internal/worker/ytdlp"
	"github.com/tuskar2321/quoker/internal/worker/ytdlp/ytdlptest"
)

func TestMain(m *testing.M) {
	os.Exit(ytdlptest.Main(m))
}

// fakeServices plays the platform CDN (thumbnails), the core's internal
// upload endpoints and the Bot API on one TLS server.
type fakeServices struct {
	mu         sync.Mutex
	srv        *httptest.Server
	thumbs     map[string][]byte
	files      map[string][]byte
	videos     []int // sizes of videos posted to sendVideo
	previews   int
	tgStatus   int
	tgBody     string
	thumbImage []byte
}

func newFakeServices(t *testing.T) *fakeServices {
	t.Helper()
	f := &fakeServices{thumbs: map[string][]byte{}, files: map[string][]byte{}, thumbImage: testThumb()}
	f.srv = httptest.NewTLSServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeServices) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := r.URL.Path
	switch {
	case p == "/cdn/thumb.png":
		_, _ = w.Write(f.thumbImage)
	case strings.HasPrefix(p, "/internal/v1/thumbs/"):
		b, _ := io.ReadAll(r.Body)
		f.thumbs[strings.TrimPrefix(p, "/internal/v1/thumbs/")] = b
	case strings.HasPrefix(p, "/internal/v1/files/"):
		id := strings.TrimPrefix(p, "/internal/v1/files/")
		if r.Method == http.MethodHead {
			if b, ok := f.files[id]; ok {
				w.Header().Set("Content-Length", strconv.Itoa(len(b)))
				return
			}
			w.WriteHeader(http.StatusNotFound)
			return
		}
		b, _ := io.ReadAll(r.Body)
		f.files[id] = append(f.files[id], b...)
	case strings.HasSuffix(p, "/sendVideo"):
		if f.tgStatus != 0 {
			w.WriteHeader(f.tgStatus)
			_, _ = io.WriteString(w, f.tgBody)
			return
		}
		_ = r.ParseMultipartForm(1 << 20) //nolint:gosec // test server
		if fh := r.MultipartForm.File["video"]; len(fh) == 1 {
			f.videos = append(f.videos, int(fh[0].Size))
		}
		if len(r.MultipartForm.File["thumb"]) == 1 {
			f.previews++
		}
		_, _ = fmt.Fprintf(w, `{"ok":true,"result":{"message_id":%d}}`, 100+len(f.videos))
	case strings.HasSuffix(p, "/getChat"):
		_, _ = io.WriteString(w, `{"ok":true,"result":{"id":-5,"title":"G"}}`)
	default:
		http.NotFound(w, r)
	}
}

// testThumb is a 1080x1920 PNG, larger than every target size.
var testThumb = sync.OnceValue(func() []byte {
	src := image.NewRGBA(image.Rect(0, 0, 1080, 1920))
	for i := range src.Pix {
		src.Pix[i] = byte(i * 31 % 251)
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, src)
	return buf.Bytes()
})

type env struct {
	h     *tasks.Handlers
	fake  *fakeServices
	cfg   *linkv1.Config
	calls func() [][]string
}

func newEnv(t *testing.T, sc ytdlptest.Scenario) *env {
	t.Helper()
	fake := newFakeServices(t)
	sc.InfoJSON = strings.ReplaceAll(sc.InfoJSON, "THUMB", fake.srv.URL+"/cdn/thumb.png")
	bin, calls := ytdlptest.Install(t, sc)
	cache, err := tmpcache.New(filepath.Join(t.TempDir(), "files"), 1<<30)
	if err != nil {
		t.Fatal(err)
	}
	hc := fake.srv.Client()
	h := tasks.New(tasks.Deps{
		YtDlp:    ytdlp.New(bin, "UA", t.TempDir()),
		Telegram: telegram.New(hc),
		Upload:   upload.New(hc).WithRetryPause(0),
		Cache:    cache,
		Web:      hc,
		Log:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	return &env{h: h, fake: fake, calls: calls, cfg: &linkv1.Config{
		BotToken:   "1:TEST",
		BotApiBase: fake.srv.URL,
		UploadBase: fake.srv.URL + "/internal/v1",
	}}
}

func noProgress(int32) {}

const tiktokInfo = `{"id":"42","extractor_key":"TikTok","webpage_url":"https://www.tiktok.com/@max/video/42",
 "uploader":"max","description":"hello ","duration":12.6,"thumbnail":"THUMB","formats":[
  {"format_id":"download","format_note":"watermarked","vcodec":"h264","acodec":"aac","height":1080},
  {"format_id":"h264_1080","vcodec":"h264","acodec":"aac","width":1080,"height":1080},
  {"format_id":"h264_720","vcodec":"h264","acodec":"aac","width":720,"height":720,"filesize":500}]}`

func TestResolve(t *testing.T) {
	e := newEnv(t, ytdlptest.Scenario{InfoJSON: tiktokInfo})
	res, err := e.h.Resolve(context.Background(), &linkv1.ResolveTask{Url: "https://vm.tiktok.com/ZM1/"}, e.cfg, noProgress)
	if err != nil {
		t.Fatal(err)
	}
	if res.Platform != "tiktok" || res.NativeId != "42" || res.Author != "max" || res.Caption != "hello" ||
		res.DurationS != 13 || !res.NoWatermark || res.Height != 1080 || !res.ThumbnailUploaded {
		t.Fatalf("result = %+v", res)
	}
	cover := e.fake.thumbs["tiktok:42"]
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(cover))
	if err != nil || max(cfg.Width, cfg.Height) > 720 || len(cover) > 512<<10 {
		t.Fatalf("cover %dx%d %d bytes: %v", cfg.Width, cfg.Height, len(cover), err)
	}
}

func TestResolve_coverFailureIsNotFatal(t *testing.T) {
	info := strings.ReplaceAll(tiktokInfo, `"thumbnail":"THUMB"`, `"thumbnail":"http://insecure/x.png"`)
	e := newEnv(t, ytdlptest.Scenario{InfoJSON: info})
	res, err := e.h.Resolve(context.Background(), &linkv1.ResolveTask{Url: "u", VideoId: "tiktok:42"}, e.cfg, noProgress)
	if err != nil || res.ThumbnailUploaded {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
}

func TestResolve_unsupported(t *testing.T) {
	tests := map[string]string{
		"carousel":  `{"_type":"playlist","id":"x","extractor_key":"Instagram"}`,
		"too long":  `{"id":"1","extractor_key":"TikTok","duration":601,"formats":[{"format_id":"a","vcodec":"h264","height":720}]}`,
		"no videos": `{"id":"1","extractor_key":"TikTok","formats":[{"format_id":"a","vcodec":"none","acodec":"aac"}]}`,
	}
	for name, info := range tests {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t, ytdlptest.Scenario{InfoJSON: info})
			_, err := e.h.Resolve(context.Background(), &linkv1.ResolveTask{Url: "u"}, e.cfg, noProgress)
			_ = assertCode(t, err, errs.UnsupportedContent)
		})
	}
}

func TestSendVideo_stepsDownUntilItFits(t *testing.T) {
	e := newEnv(t, ytdlptest.Scenario{InfoJSON: tiktokInfo, FileBytes: map[string]int{"h264_1080": 2000, "h264_720": 500}})
	e.cfg.MaxSendBytes = 1000
	task := &linkv1.SendVideoTask{VideoId: "tiktok:42", Url: "https://www.tiktok.com/@max/video/42", ChatId: -5, CaptionHtml: "hi"}

	res, err := e.h.SendVideo(context.Background(), task, e.cfg, noProgress)
	if err != nil {
		t.Fatal(err)
	}
	if res.MessageId != 101 || res.FormatId != "h264_720" || !slices.Equal(e.fake.videos, []int{500}) || e.fake.previews != 1 {
		t.Fatalf("res = %+v videos = %v previews = %d", res, e.fake.videos, e.fake.previews)
	}
	if got := downloads(e.calls()); !slices.Equal(got, []string{"h264_1080", "h264_720"}) {
		t.Fatalf("downloads = %v", got)
	}

	// A second send of the same video reuses the cached file.
	before := len(e.calls())
	if _, err := e.h.SendVideo(context.Background(), task, e.cfg, noProgress); err != nil {
		t.Fatal(err)
	}
	if len(e.calls()) != before {
		t.Fatalf("yt-dlp called again on cache hit: %v", e.calls()[before:])
	}
}

func TestSendVideo_errors(t *testing.T) {
	wmOnly := `{"id":"42","extractor_key":"TikTok","formats":[{"format_id":"download","format_note":"watermarked","vcodec":"h264","height":720}]}`
	tests := []struct {
		name     string
		sc       ytdlptest.Scenario
		maxBytes int64
		tgStatus int
		tgBody   string
		want     errs.Code
		tgCode   int32
	}{
		{"only watermarked", ytdlptest.Scenario{InfoJSON: wmOnly}, 0, 0, "", errs.UpstreamError, 0},
		{"too large", ytdlptest.Scenario{InfoJSON: tiktokInfo, FileBytes: map[string]int{"h264_1080": 5000}}, 100, 0, "", errs.TooLarge, 0},
		{"bot kicked", ytdlptest.Scenario{InfoJSON: tiktokInfo}, 0, 403,
			`{"ok":false,"error_code":403,"description":"Forbidden: bot was kicked"}`, errs.TargetForbidden, 403},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t, tt.sc)
			e.cfg.MaxSendBytes = tt.maxBytes
			e.fake.tgStatus, e.fake.tgBody = tt.tgStatus, tt.tgBody
			_, err := e.h.SendVideo(context.Background(), &linkv1.SendVideoTask{VideoId: "tiktok:42", Url: "u", ChatId: 1}, e.cfg, noProgress)
			te := assertCode(t, err, tt.want)
			if te.TelegramCode != tt.tgCode {
				t.Fatalf("telegram code = %d, want %d", te.TelegramCode, tt.tgCode)
			}
		})
	}
}

func TestDownload(t *testing.T) {
	e := newEnv(t, ytdlptest.Scenario{InfoJSON: tiktokInfo, FileBytes: map[string]int{"h264_1080": 3000}})
	var reported []int32
	var mu sync.Mutex
	res, err := e.h.Download(context.Background(), &linkv1.DownloadTask{VideoId: "tiktok:42", Url: "u"}, e.cfg, func(p int32) {
		mu.Lock()
		reported = append(reported, p)
		mu.Unlock()
	})
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(make([]byte, 3000))
	if res.SizeBytes != 3000 || res.Sha256 != hex.EncodeToString(sum[:]) || res.FormatId != "h264_1080" {
		t.Fatalf("res = %+v", res)
	}
	if len(e.fake.files["tiktok:42"]) != 3000 {
		t.Fatalf("core got %d bytes", len(e.fake.files["tiktok:42"]))
	}
	mu.Lock()
	defer mu.Unlock()
	if len(reported) == 0 || reported[len(reported)-1] != 100 {
		t.Fatalf("progress = %v", reported)
	}
}

func TestBotCall(t *testing.T) {
	e := newEnv(t, ytdlptest.Scenario{})
	res, err := e.h.BotCall(context.Background(), &linkv1.BotCallTask{Method: "getChat", ParamsJson: `{"chat_id":-5}`}, e.cfg)
	if err != nil || res.ResponseJson != `{"id":-5,"title":"G"}` {
		t.Fatalf("res = %v, err = %v", res, err)
	}
	_, err = e.h.BotCall(context.Background(), &linkv1.BotCallTask{Method: "../getMe"}, e.cfg)
	_ = assertCode(t, err, errs.Internal)
}

func assertCode(t *testing.T, err error, want errs.Code) *runner.TaskError {
	t.Helper()
	var te *runner.TaskError
	if !errors.As(err, &te) || te.Code != string(want) {
		t.Fatalf("err = %v, want task error %s", err, want)
	}
	return te
}

func downloads(calls [][]string) []string {
	var out []string
	for _, c := range calls {
		if i := slices.Index(c, "-f"); i >= 0 {
			out = append(out, c[i+1])
		}
	}
	return out
}
