package ytdlp_test

import (
	"context"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/tuskar2321/quoker/internal/shared/errs"
	"github.com/tuskar2321/quoker/internal/worker/ytdlp"
	"github.com/tuskar2321/quoker/internal/worker/ytdlp/ytdlptest"
)

func TestMain(m *testing.M) {
	os.Exit(ytdlptest.Main(m))
}

const tiktokJSON = `{"id":"42","extractor_key":"TikTok","webpage_url":"https://www.tiktok.com/@a/video/42",
 "uploader":"a","duration":12.6,"formats":[
  {"format_id":"download","format_note":"watermarked","vcodec":"h264","acodec":"aac","height":1080,"filesize":100},
  {"format_id":"h264_720","vcodec":"h264","acodec":"aac","height":720,"filesize":50}]}`

func runner(t *testing.T, sc ytdlptest.Scenario) (*ytdlp.Runner, func() [][]string) {
	t.Helper()
	bin, calls := ytdlptest.Install(t, sc)
	return ytdlp.New(bin, "UA", t.TempDir()), calls
}

func TestMetadata(t *testing.T) {
	r, calls := runner(t, ytdlptest.Scenario{InfoJSON: tiktokJSON})
	info, err := r.Metadata(context.Background(), "https://www.tiktok.com/@a/video/42", "cookie-data")
	if err != nil {
		t.Fatal(err)
	}
	if info.ID != "42" || info.Platform() != "tiktok" || info.UsedCookies {
		t.Fatalf("info = %+v", info)
	}
	got := calls()
	if len(got) != 1 || !slices.Contains(got[0], "-J") || slices.Contains(got[0], "--cookies") {
		t.Fatalf("calls = %v", got)
	}
	// The URL comes after "--" so it can never be read as an option.
	if args := got[0]; args[len(args)-2] != "--" {
		t.Fatalf("args = %v", args)
	}
}

func TestMetadata_errors(t *testing.T) {
	tests := []struct {
		name    string
		sc      ytdlptest.Scenario
		url     string
		cookies string
		want    errs.Code
	}{
		{"private", ytdlptest.Scenario{ExitCode: 1, Stderr: "ERROR: [TikTok] 1: This video is private"},
			"https://www.tiktok.com/@a/video/1", "", errs.PrivateVideo},
		{"carousel", ytdlptest.Scenario{ExitCode: 1, Stderr: "ERROR: There is no video in this post"},
			"https://www.instagram.com/p/x/", "", errs.UnsupportedContent},
		{"429", ytdlptest.Scenario{ExitCode: 1, Stderr: "ERROR: HTTP Error 429: Too Many Requests"},
			"https://www.tiktok.com/@a/video/1", "", errs.RateLimited},
		{"other", ytdlptest.Scenario{ExitCode: 1, Stderr: "ERROR: boom"},
			"https://www.tiktok.com/@a/video/1", "", errs.UpstreamError},
		{"instagram without cookies", ytdlptest.Scenario{LoginUnlessCookies: true, InfoJSON: "{}"},
			"https://www.instagram.com/reel/x/", "", errs.LoginRequired},
		{"cookies are instagram-only", ytdlptest.Scenario{LoginUnlessCookies: true, InfoJSON: "{}"},
			"https://www.tiktok.com/@a/video/1", "c", errs.LoginRequired},
		{"bad json", ytdlptest.Scenario{InfoJSON: "not json"},
			"https://www.tiktok.com/@a/video/1", "", errs.UpstreamError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, _ := runner(t, tt.sc)
			_, err := r.Metadata(context.Background(), tt.url, tt.cookies)
			if !errs.Is(err, tt.want) {
				t.Fatalf("err = %v, want %s", err, tt.want)
			}
		})
	}
}

func TestMetadata_cookiesRetry(t *testing.T) {
	r, calls := runner(t, ytdlptest.Scenario{LoginUnlessCookies: true, InfoJSON: `{"id":"x","extractor_key":"Instagram"}`})
	info, err := r.Metadata(context.Background(), "https://www.instagram.com/reel/x/", "# Netscape HTTP Cookie File")
	if err != nil {
		t.Fatal(err)
	}
	if !info.UsedCookies {
		t.Fatal("UsedCookies = false")
	}
	got := calls()
	if len(got) != 2 || !slices.Contains(got[1], "--cookies") {
		t.Fatalf("calls = %v", got)
	}
	cookieFile := got[1][slices.Index(got[1], "--cookies")+1]
	if _, err := os.Stat(cookieFile); !os.IsNotExist(err) {
		t.Fatalf("cookies file left behind: %v", err)
	}
}

func TestDownload(t *testing.T) {
	r, calls := runner(t, ytdlptest.Scenario{FileBytes: map[string]int{"h264_720": 2048}})
	dir := t.TempDir()
	path, err := r.Download(context.Background(), "https://www.tiktok.com/@a/video/42", "h264_720", dir, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil || st.Size() != 2048 {
		t.Fatalf("file %s: %v %v", path, st, err)
	}
	if args := calls()[0]; !slices.Contains(args, "--merge-output-format") {
		t.Fatalf("args = %v", args)
	}
}

func TestCancel(t *testing.T) {
	r, _ := runner(t, ytdlptest.Scenario{Sleep: 10 * time.Second, InfoJSON: "{}"})
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := r.Metadata(ctx, "https://www.tiktok.com/@a/video/1", "")
	if !errs.Is(err, errs.Cancelled) {
		t.Fatalf("err = %v, want cancelled", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("process was not killed promptly")
	}
}

func TestVersion(t *testing.T) {
	r, _ := runner(t, ytdlptest.Scenario{})
	v, err := r.Version(context.Background())
	if err != nil || v != "2026.10.01" {
		t.Fatalf("version = %q, %v", v, err)
	}
}

func TestChoose(t *testing.T) {
	info := &ytdlp.Info{Formats: []ytdlp.Format{
		{ID: "wm", Note: "watermarked", VCodec: "h264", ACodec: "aac", Height: 1080, Filesize: 10},
		{ID: "hevc1080", VCodec: "h265", ACodec: "aac", Height: 1080, Filesize: 80},
		{ID: "avc1080", VCodec: "avc1.640028", ACodec: "none", Height: 1080, Filesize: 60},
		{ID: "avc720", VCodec: "avc1", ACodec: "aac", Height: 720, Filesize: 30},
		{ID: "a-opus", VCodec: "none", ACodec: "opus", ABR: 160, Filesize: 5},
		{ID: "a-aac", VCodec: "none", ACodec: "mp4a.40.2", ABR: 128, Filesize: 4},
	}}
	tests := []struct {
		name      string
		maxHeight int
		maxBytes  int64
		noWM      bool
		want      string
		wantOK    bool
	}{
		{"h264 preferred at same height, paired with aac", 1080, 100, true, "avc1080+a-aac", true},
		{"size limit drops to lower formats", 1080, 50, true, "avc720", true},
		{"height cap", 720, 100, true, "avc720", true},
		{"watermarked allowed when not excluded", 1080, 20, false, "wm", true},
		{"nothing fits", 1080, 10, true, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, ok := info.Choose(tt.maxHeight, tt.maxBytes, tt.noWM)
			if ok != tt.wantOK || c.Selector != tt.want {
				t.Fatalf("Choose = %q %v, want %q %v", c.Selector, ok, tt.want, tt.wantOK)
			}
		})
	}
	if !info.HasCleanVideo() {
		t.Fatal("HasCleanVideo = false")
	}
}

func TestNextHeight(t *testing.T) {
	for in, want := range map[int]int{1080: 720, 1079: 720, 720: 540, 540: 480, 480: 0, 2160: 1080} {
		if got := ytdlp.NextHeight(in); got != want {
			t.Errorf("NextHeight(%d) = %d, want %d", in, got, want)
		}
	}
}

// TestDownload_maxFilesize: an oversized format is refused by yt-dlp instead
// of being downloaded whole (REVIEW M20).
func TestDownload_maxFilesize(t *testing.T) {
	r, calls := runner(t, ytdlptest.Scenario{FileBytes: map[string]int{"big": 5000}})
	dir := t.TempDir()
	_, err := r.Download(context.Background(), "https://www.tiktok.com/@a/video/42", "big", dir, "", 1000)
	if !errs.Is(err, errs.TooLarge) {
		t.Fatalf("err = %v, want too_large", err)
	}
	if args := calls()[0]; args[slices.Index(args, "--max-filesize")+1] != "1000" {
		t.Fatalf("args = %v", args)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("oversized file was written: %v", entries)
	}
}

// TestMetadata_deletedInstagramPost: when cookies do not unlock a post it is
// gone, not a login problem, and no cookie alert should follow (REVIEW L12).
func TestMetadata_deletedInstagramPost(t *testing.T) {
	r, calls := runner(t, ytdlptest.Scenario{GoneEvenWithCookie: true})
	_, err := r.Metadata(context.Background(), "https://www.instagram.com/reel/x/", "cookies")
	if !errs.Is(err, errs.PrivateVideo) {
		t.Fatalf("err = %v, want private_video", err)
	}
	if len(calls()) != 2 {
		t.Fatalf("calls = %d, want a retry with cookies", len(calls()))
	}
	// Without cookies to try, it stays a login problem for the admin.
	r2, _ := runner(t, ytdlptest.Scenario{GoneEvenWithCookie: true})
	if _, err := r2.Metadata(context.Background(), "https://www.instagram.com/reel/x/", ""); !errs.Is(err, errs.LoginRequired) {
		t.Fatalf("no cookies: err = %v", err)
	}
}
