package videos_test

import (
	"context"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	linkv1 "github.com/tuskar2321/quoker/gen/quoker/link/v1"
	"github.com/tuskar2321/quoker/internal/core/queue"
	"github.com/tuskar2321/quoker/internal/core/store"
	"github.com/tuskar2321/quoker/internal/core/videos"
	"github.com/tuskar2321/quoker/internal/shared/errs"
)

type fakeClock struct{ t time.Time }

func (c *fakeClock) Now() time.Time          { return c.t }
func (c *fakeClock) Advance(d time.Duration) { c.t = c.t.Add(d) }

func openTest(t *testing.T) (*store.Store, *queue.Store, *videos.Service, *fakeClock) {
	t.Helper()
	p := t.TempDir() + "/quoker.db"
	st, err := store.Open(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	clk := &fakeClock{t: time.Now()}
	q := queue.New(st, clk.Now)
	// 500ms resolveWait lets polls return quickly in tests.
	svc := videos.New(st, q, clk.Now).WithResolveWait(500 * time.Millisecond)
	return st, q, svc, clk
}

// insertVideo directly inserts a resolved video for cache-hit tests.
func insertVideo(t *testing.T, st *store.Store, now time.Time, id, platform, nativeID, canonicalURL string) {
	t.Helper()
	expires := now.Add(24 * time.Hour)
	_, err := st.DB().ExecContext(context.Background(), `
		INSERT INTO videos
		  (id, platform, native_id, canonical_url, file_status, resolved_at, expires_at, last_access_at)
		VALUES (?,?,?,?,'none',?,?,?)`,
		id, platform, nativeID, canonicalURL,
		now.UnixMilli(), expires.UnixMilli(), now.UnixMilli())
	if err != nil {
		t.Fatalf("insertVideo: %v", err)
	}
}

func insertAlias(t *testing.T, st *store.Store, now time.Time, key, videoID string) {
	t.Helper()
	_, err := st.DB().ExecContext(context.Background(),
		`INSERT OR IGNORE INTO video_aliases (url_normalized, video_id, created_at) VALUES (?,?,?)`,
		key, videoID, now.UnixMilli())
	if err != nil {
		t.Fatalf("insertAlias: %v", err)
	}
}

// completeTask marks the task done with a serialised ResolveResult blob.
func completeTask(t *testing.T, st *store.Store, now time.Time, taskID string, r *linkv1.ResolveResult) {
	t.Helper()
	blob, err := proto.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	_, err = st.DB().ExecContext(context.Background(),
		`UPDATE tasks SET status='done', result=?, updated_at=? WHERE id=?`,
		blob, now.UnixMilli(), taskID)
	if err != nil {
		t.Fatalf("completeTask: %v", err)
	}
}

func failTask(t *testing.T, st *store.Store, now time.Time, taskID, code, msg string) {
	t.Helper()
	_, err := st.DB().ExecContext(context.Background(),
		`UPDATE tasks SET status='failed', error_code=?, error_message=?, updated_at=? WHERE id=?`,
		code, msg, now.UnixMilli(), taskID)
	if err != nil {
		t.Fatalf("failTask: %v", err)
	}
}

// --- tests ---

func TestResolve_cacheHit_byVideoID(t *testing.T) {
	st, _, svc, clk := openTest(t)
	ctx := context.Background()

	insertVideo(t, st, clk.Now(), "tiktok:123", "tiktok", "123", "https://www.tiktok.com/@user/video/123")

	res, err := svc.Resolve(ctx, "https://www.tiktok.com/@user/video/123", 0)
	if err != nil {
		t.Fatal(err)
	}
	if res.Video == nil {
		t.Fatal("expected cache hit, got nil Video")
	}
	if res.Video.ID != "tiktok:123" {
		t.Fatalf("got ID %q, want tiktok:123", res.Video.ID)
	}
	if res.TaskID != "" {
		t.Fatal("expected no task id on cache hit")
	}
}

func TestResolve_cacheHit_byAlias(t *testing.T) {
	st, _, svc, clk := openTest(t)
	ctx := context.Background()

	insertVideo(t, st, clk.Now(), "tiktok:999", "tiktok", "999", "https://www.tiktok.com/@user/video/999")
	insertAlias(t, st, clk.Now(), "https://vm.tiktok.com/abc123", "tiktok:999")

	res, err := svc.Resolve(ctx, "https://vm.tiktok.com/abc123", 0)
	if err != nil {
		t.Fatal(err)
	}
	if res.Video == nil || res.Video.ID != "tiktok:999" {
		t.Fatalf("alias cache hit failed: %+v", res)
	}
}

// TestResolve_enqueueAndReturns202 verifies that when no worker completes the
// task within resolveWait (500ms in test), Resolve returns a TaskID (202 path).
func TestResolve_enqueueAndReturns202(t *testing.T) {
	_, _, svc, _ := openTest(t)
	ctx := context.Background()

	res, err := svc.Resolve(ctx, "https://vm.tiktok.com/xyz789", 0)
	if err != nil {
		t.Fatal(err)
	}
	if res.TaskID == "" {
		t.Fatal("expected non-empty TaskID (202 path)")
	}
	if res.Video != nil {
		t.Fatal("expected nil Video on 202 path")
	}
}

// TestResolve_dedup verifies two concurrent calls return the same task id.
func TestResolve_dedup(t *testing.T) {
	_, q, svc, _ := openTest(t)
	ctx := context.Background()

	// First call enqueues (callerID=0 → no FK on user_id).
	res1, err := svc.Resolve(ctx, "https://vm.tiktok.com/dedup1", 0)
	if err != nil {
		t.Fatal(err)
	}
	// Second call with the same URL hits the dedup path and returns the same task.
	res2, err := svc.Resolve(ctx, "https://vm.tiktok.com/dedup1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if res1.TaskID != res2.TaskID {
		t.Fatalf("dedup failed: got %q and %q", res1.TaskID, res2.TaskID)
	}
	// Confirm only one task in the queue.
	task, err := q.Lease(ctx, "w")
	if err != nil {
		t.Fatal(err)
	}
	_, err = q.Lease(ctx, "w")
	if err == nil {
		t.Fatal("expected no second task")
	}
	_ = task
}

func TestResolve_workerFailure(t *testing.T) {
	st, _, svc, clk := openTest(t)
	ctx := context.Background()

	// Enqueue a task via the queue, fail it immediately, then verify GetTask reflects it.
	tid, err := svc.Q().Enqueue(ctx, queue.EnqueueParams{
		Kind:     queue.KindResolve,
		DedupKey: "resolve:priv-unique-1",
		Payload:  []byte{},
	})
	if err != nil {
		t.Fatal(err)
	}
	failTask(t, st, clk.Now(), tid, "private_video", "account is private")

	task, _, err := svc.GetTask(ctx, tid)
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != "failed" {
		t.Fatalf("expected failed, got %q", task.Status)
	}
	if task.ErrCode != "private_video" {
		t.Fatalf("expected private_video error code, got %q", task.ErrCode)
	}
}

func TestResolve_unsupportedURL(t *testing.T) {
	_, _, svc, _ := openTest(t)
	_, err := svc.Resolve(context.Background(), "https://youtube.com/watch?v=abc", 0)
	if err == nil {
		t.Fatal("expected error for unsupported URL")
	}
}

func TestApplyResolveResult(t *testing.T) {
	st, _, svc, clk := openTest(t)
	ctx := context.Background()

	r := &linkv1.ResolveResult{
		Platform:          "tiktok",
		NativeId:          "42",
		CanonicalUrl:      "https://www.tiktok.com/@x/video/42",
		Author:            "x",
		Caption:           "cool",
		DurationS:         15,
		SizeBytes:         1000000,
		Width:             1080,
		Height:            1920,
		NoWatermark:       true,
		ThumbnailUploaded: true,
	}
	if err := svc.ApplyResolveResult(ctx, "task-1", r, "https://tiktok.com/v/42"); err != nil {
		t.Fatal(err)
	}

	v, err := svc.Get(ctx, "tiktok:42")
	if err != nil {
		t.Fatal(err)
	}
	if v.Author != "x" || v.Caption != "cool" || v.DurationS != 15 {
		t.Fatalf("unexpected video fields: %+v", v)
	}
	if !v.HasThumbnail || !v.NoWatermark {
		t.Fatalf("flags: has_thumbnail=%v no_watermark=%v", v.HasThumbnail, v.NoWatermark)
	}

	// Alias saved.
	var aliasID string
	if err = st.DB().QueryRowContext(ctx,
		`SELECT video_id FROM video_aliases WHERE url_normalized='https://tiktok.com/v/42'`).Scan(&aliasID); err != nil {
		t.Fatal("alias not found:", err)
	}
	if aliasID != "tiktok:42" {
		t.Fatalf("alias video_id = %q, want tiktok:42", aliasID)
	}

	// history_videos saved.
	var histPlatform string
	if err = st.DB().QueryRowContext(ctx,
		`SELECT platform FROM history_videos WHERE video_id='tiktok:42'`).Scan(&histPlatform); err != nil {
		t.Fatal("history_videos not found:", err)
	}
	_ = clk
}

func TestGet_notFound(t *testing.T) {
	_, _, svc, _ := openTest(t)
	_, err := svc.Get(context.Background(), "tiktok:nonexistent")
	if !errs.Is(err, errs.NotFound) {
		t.Fatalf("expected not_found, got %v", err)
	}
}

func TestGet_touchExtendsExpiry(t *testing.T) {
	st, _, svc, clk := openTest(t)
	ctx := context.Background()

	insertVideo(t, st, clk.Now(), "tiktok:5", "tiktok", "5", "https://www.tiktok.com/@u/video/5")

	clk.Advance(2 * time.Hour)
	v, err := svc.Get(ctx, "tiktok:5")
	if err != nil {
		t.Fatal(err)
	}
	want := clk.Now().Add(24 * time.Hour).Unix()
	if v.ExpiresAt.Unix() != want {
		t.Fatalf("expires_at = %v, want ~%v", v.ExpiresAt, time.Unix(want, 0))
	}
}

func TestGetTask_resolveResult(t *testing.T) {
	st, q, svc, clk := openTest(t)
	ctx := context.Background()

	taskID, err := q.Enqueue(ctx, queue.EnqueueParams{
		Kind:     queue.KindResolve,
		DedupKey: "resolve:https://tiktok.com/v/88",
		Payload:  []byte{},
	})
	if err != nil {
		t.Fatal(err)
	}

	r := &linkv1.ResolveResult{Platform: "tiktok", NativeId: "88", CanonicalUrl: "https://www.tiktok.com/@u/video/88"}
	if err := svc.ApplyResolveResult(ctx, taskID, r, ""); err != nil {
		t.Fatal(err)
	}
	completeTask(t, st, clk.Now(), taskID, r)

	task, v, err := svc.GetTask(ctx, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != "done" {
		t.Fatalf("status = %q, want done", task.Status)
	}
	if v == nil || v.ID != "tiktok:88" {
		t.Fatalf("expected video tiktok:88, got %v", v)
	}
}

// TestResolve_answersBeforeRequestDeadline: the wait must end before the
// request's own timeout so the client gets 202 with the task id (REVIEW H1).
func TestResolve_answersBeforeRequestDeadline(t *testing.T) {
	st, err := store.Open(context.Background(), t.TempDir()+"/q.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	svc := videos.New(st, queue.New(st, nil), nil) // default 15 s wait

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	start := time.Now()
	res, err := svc.Resolve(ctx, "https://vm.tiktok.com/ZMabc/", 0)
	if err != nil || res.TaskID == "" || res.Video != nil {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
	if took := time.Since(start); took > 2*time.Second || ctx.Err() != nil {
		t.Fatalf("returned after %s, ctx err %v: no time left to answer", took, ctx.Err())
	}
}
