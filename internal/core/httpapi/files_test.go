package httpapi

import (
	"bytes"
	"context"
	"net/http"
	"testing"

	"github.com/tuskar2321/quoker/internal/core/accounts"
	"github.com/tuskar2321/quoker/internal/core/queue"
)

func (e *env) addVideo(id, platform string) {
	e.t.Helper()
	ms := e.now.UnixMilli()
	if _, err := e.st.DB().ExecContext(context.Background(),
		`INSERT INTO videos (id, platform, native_id, canonical_url, author, file_status, resolved_at, expires_at, last_access_at)
		 VALUES (?, ?, '1', 'https://www.tiktok.com/@a/video/1', 'a', 'none', ?, ?, ?)`,
		id, platform, ms, ms+86_400_000, ms); err != nil {
		e.t.Fatal(err)
	}
}

func TestVideoShape(t *testing.T) {
	e := newEnv(t)
	tok, _ := e.user(accounts.RoleUser)
	e.addVideo("tiktok:1", "tiktok")
	if err := e.files.PutThumb(context.Background(), "tiktok:1", bytes.NewReader([]byte{0xFF, 0xD8, 0xFF, 0})); err != nil {
		t.Fatal(err)
	}
	r := e.do(req{method: http.MethodGet, path: "/v1/videos/tiktok:1", token: tok})
	v, _ := r.body["video"].(map[string]any)
	file, _ := v["file"].(map[string]any)
	if r.status != http.StatusOK || v["source_url"] != "https://www.tiktok.com/@a/video/1" ||
		v["thumbnail_url"] != "/v1/videos/tiktok:1/thumbnail" || v["no_watermark"] != false ||
		file["status"] != "none" || v["caption"] != nil {
		t.Fatalf("video = %s", r.raw)
	}

	th := e.do(req{method: http.MethodGet, path: "/v1/videos/tiktok:1/thumbnail", token: tok})
	if th.status != http.StatusOK || th.header.Get("Content-Type") != "image/jpeg" || len(th.raw) != 4 {
		t.Fatalf("thumbnail: %d %q", th.status, th.header.Get("Content-Type"))
	}

	e.addVideo("instagram:x", "instagram")
	r = e.do(req{method: http.MethodGet, path: "/v1/videos/instagram:x", token: tok})
	if v := r.body["video"].(map[string]any); v["no_watermark"] != nil || v["thumbnail_url"] != nil {
		t.Fatalf("instagram video = %s", r.raw)
	}
	if r := e.do(req{method: http.MethodGet, path: "/v1/videos/instagram:x/thumbnail", token: tok}); r.status != http.StatusNotFound {
		t.Fatalf("missing thumbnail: %d", r.status)
	}
}

func TestPrepareAndServe(t *testing.T) {
	e := newEnv(t)
	tok, _ := e.user(accounts.RoleUser)
	e.addVideo("tiktok:1", "tiktok")
	ctx := context.Background()

	first := e.do(req{method: http.MethodPost, path: "/v1/videos/tiktok:1/prepare", token: tok})
	second := e.do(req{method: http.MethodPost, path: "/v1/videos/tiktok:1/prepare", token: tok})
	taskID, _ := first.body["task_id"].(string)
	if first.status != http.StatusAccepted || taskID == "" || second.body["task_id"] != taskID {
		t.Fatalf("prepare: %s / %s", first.raw, second.raw)
	}
	task := e.do(req{method: http.MethodGet, path: "/v1/tasks/" + taskID, token: tok})
	if task.body["kind"] != "download" || task.body["status"] != "queued" || task.body["error"] != nil {
		t.Fatalf("task = %s", task.raw)
	}
	if r := e.do(req{method: http.MethodGet, path: "/v1/videos/tiktok:1/file", token: tok}); r.errCode() != "file_not_ready" {
		t.Fatalf("file before ready: %d %s", r.status, r.raw)
	}

	// The worker uploads and reports; the core marks the file ready.
	data := []byte("0123456789")
	if _, err := e.files.Append("tiktok:1", 0, int64(len(data)), bytes.NewReader(data)); err != nil {
		t.Fatal(err)
	}
	if err := e.videos.SetFileReady(ctx, "tiktok:1", int64(len(data)), "abc"); err != nil {
		t.Fatal(err)
	}

	ready := e.do(req{method: http.MethodPost, path: "/v1/videos/tiktok:1/prepare", token: tok})
	if f, _ := ready.body["file"].(map[string]any); ready.status != http.StatusOK || f["status"] != "ready" || f["size_bytes"] != float64(10) {
		t.Fatalf("prepare when ready: %s", ready.raw)
	}
	part := e.do(req{method: http.MethodGet, path: "/v1/videos/tiktok:1/file", token: tok,
		header: map[string]string{"Range": "bytes=2-5"}})
	if part.status != http.StatusPartialContent || part.raw != "2345" || part.header.Get("ETag") != `"abc"` ||
		part.header.Get("Content-Type") != "video/mp4" {
		t.Fatalf("range: %d %q %v", part.status, part.raw, part.header)
	}

	if r := e.do(req{method: http.MethodPost, path: "/v1/videos/tiktok:1/downloaded", token: tok}); r.status != http.StatusNoContent {
		t.Fatalf("downloaded: %d %s", r.status, r.raw)
	}
	var n int
	_ = e.st.DB().QueryRow(`SELECT COUNT(*) FROM history WHERE video_id = 'tiktok:1' AND action = 'downloaded'`).Scan(&n)
	if n != 1 {
		t.Fatalf("history rows = %d", n)
	}
}

func TestFileStreamingIsNotCutByTimeout(t *testing.T) {
	// Guard against moving the file route back under the JSON timeout: a
	// slow reader must still get the whole body.
	e := newEnv(t)
	tok, _ := e.user(accounts.RoleUser)
	e.addVideo("tiktok:1", "tiktok")
	data := bytes.Repeat([]byte("v"), 1<<20)
	_, _ = e.files.Append("tiktok:1", 0, int64(len(data)), bytes.NewReader(data))
	_ = e.videos.SetFileReady(context.Background(), "tiktok:1", int64(len(data)), "")
	r := e.do(req{method: http.MethodGet, path: "/v1/videos/tiktok:1/file", token: tok})
	if r.status != http.StatusOK || len(r.raw) != len(data) {
		t.Fatalf("status %d, %d bytes", r.status, len(r.raw))
	}
}

func TestTaskFailedShape(t *testing.T) {
	e := newEnv(t)
	tok, _ := e.user(accounts.RoleUser)
	ctx := context.Background()
	id, err := e.queue.Enqueue(ctx, queue.EnqueueParams{Kind: queue.KindResolve, Payload: []byte{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.queue.Lease(ctx, "w"); err != nil {
		t.Fatal(err)
	}
	if r := e.do(req{method: http.MethodGet, path: "/v1/tasks/" + id, token: tok}); r.body["status"] != "running" {
		t.Fatalf("leased task = %s", r.raw)
	}
	if err := e.queue.Complete(ctx, queue.CompleteParams{TaskID: id, WorkerID: "w", ErrCode: "private_video"}); err != nil {
		t.Fatal(err)
	}
	r := e.do(req{method: http.MethodGet, path: "/v1/tasks/" + id, token: tok})
	er, _ := r.body["error"].(map[string]any)
	if r.body["status"] != "failed" || er["code"] != "private_video" || er["message"] == "" {
		t.Fatalf("failed task = %s", r.raw)
	}
}

func TestStreamLimiter(t *testing.T) {
	l := newStreamLimiter(2)
	a, ok1 := l.acquire(1)
	_, ok2 := l.acquire(1)
	_, ok3 := l.acquire(1)
	_, other := l.acquire(2)
	if !ok1 || !ok2 || ok3 || !other {
		t.Fatalf("acquire = %v %v %v %v", ok1, ok2, ok3, other)
	}
	a()
	a() // idempotent
	if _, ok := l.acquire(1); !ok {
		t.Fatal("slot not returned")
	}
}
