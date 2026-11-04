package history_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/tuskar2321/quoker/internal/core/history"
	"github.com/tuskar2321/quoker/internal/core/store"
	"github.com/tuskar2321/quoker/internal/core/videos"
	"github.com/tuskar2321/quoker/internal/shared/errs"
)

type env struct {
	t   *testing.T
	st  *store.Store
	svc *history.Service
	now time.Time
	uid int64
}

func newEnv(t *testing.T) *env {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "q.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	e := &env{t: t, st: st, now: time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)}
	e.svc = history.New(st, func() time.Time { return e.now })
	e.uid = e.user()
	return e
}

func (e *env) exec(q string, args ...any) {
	e.t.Helper()
	if _, err := e.st.DB().ExecContext(context.Background(), q, args...); err != nil {
		e.t.Fatalf("%s: %v", q, err)
	}
}

func (e *env) user() int64 {
	e.t.Helper()
	res, err := e.st.DB().ExecContext(context.Background(), `INSERT INTO users (role, status, created_at) VALUES ('user', 'active', 0)`)
	if err != nil {
		e.t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

// video stores a cached video and its history copy.
func (e *env) video(id string) {
	e.t.Helper()
	ms := e.now.UnixMilli()
	e.exec(`INSERT INTO videos (id, platform, native_id, canonical_url, author, caption, has_thumbnail, file_status, resolved_at, expires_at, last_access_at)
	        VALUES (?, 'tiktok', ?, ?, 'max', 'hello', 1, 'ready', ?, ?, ?)`, id, id[7:], "https://www.tiktok.com/@max/video/"+id[7:], ms, ms, ms)
	e.exec(`INSERT INTO history_videos (video_id, platform, canonical_url, author, duration_s) VALUES (?, 'tiktok', ?, 'max', 12)`,
		id, "https://www.tiktok.com/@max/video/"+id[7:])
}

// action records an action ago before now.
func (e *env) action(uid int64, videoID, action, title string, ago time.Duration) {
	e.t.Helper()
	e.exec(`INSERT INTO history (user_id, video_id, action, target_title, created_at) VALUES (?, ?, ?, NULLIF(?, ''), ?)`,
		uid, videoID, action, title, e.now.Add(-ago).UnixMilli())
}

func (e *env) list(cursor string, limit int) history.Page {
	e.t.Helper()
	p, err := e.svc.List(context.Background(), e.uid, cursor, limit)
	if err != nil {
		e.t.Fatal(err)
	}
	return p
}

func TestGrouping(t *testing.T) {
	e := newEnv(t)
	e.video("tiktok:a")
	e.video("tiktok:b")
	e.action(e.uid, "tiktok:a", "resolved", "", 3*time.Hour)
	e.action(e.uid, "tiktok:a", "sent", "Друзья", 2*time.Hour)
	e.action(e.uid, "tiktok:b", "downloaded", "", time.Hour)
	e.action(e.user(), "tiktok:a", "sent", "Чужое", time.Minute) // another user's

	p := e.list("", 0)
	if len(p.Items) != 2 || p.NextCursor != "" {
		t.Fatalf("items = %d, next = %q", len(p.Items), p.NextCursor)
	}
	b, a := p.Items[0], p.Items[1]
	if b.Video.ID != "tiktok:b" || a.Video.ID != "tiktok:a" {
		t.Fatalf("order = %s, %s", b.Video.ID, a.Video.ID)
	}
	if len(a.Actions) != 2 || a.Actions[0].Type != "sent" || a.Actions[0].TargetTitle != "Друзья" || a.Actions[1].Type != "resolved" {
		t.Fatalf("actions of a = %+v", a.Actions)
	}
	if !a.Video.HasThumbnail || a.Video.FileStatus != videos.FileReady || a.Video.Caption != "hello" {
		t.Fatalf("cached video = %+v", a.Video)
	}
}

func TestPagination(t *testing.T) {
	e := newEnv(t)
	for i := range 5 {
		id := fmt.Sprintf("tiktok:%d", i)
		e.video(id)
		e.action(e.uid, id, "resolved", "", time.Duration(i)*time.Minute)
	}
	var seen []string
	cursor := ""
	for pages := 0; ; pages++ {
		if pages > 5 {
			t.Fatal("pagination does not end")
		}
		p := e.list(cursor, 2)
		for _, it := range p.Items {
			seen = append(seen, it.Video.ID)
		}
		if p.NextCursor == "" {
			break
		}
		cursor = p.NextCursor
	}
	want := []string{"tiktok:0", "tiktok:1", "tiktok:2", "tiktok:3", "tiktok:4"}
	if fmt.Sprint(seen) != fmt.Sprint(want) {
		t.Fatalf("seen = %v, want %v", seen, want)
	}
	if _, err := e.svc.List(context.Background(), e.uid, "garbage!", 2); !errs.Is(err, errs.InvalidRequest) {
		t.Fatalf("bad cursor: %v", err)
	}
}

func TestVideoOutlivesCache(t *testing.T) {
	e := newEnv(t)
	e.video("tiktok:a")
	e.action(e.uid, "tiktok:a", "downloaded", "", time.Hour)
	e.exec(`DELETE FROM videos WHERE id = 'tiktok:a'`)

	v := e.list("", 0).Items[0].Video
	if v.CanonicalURL != "https://www.tiktok.com/@max/video/a" || v.Author != "max" || v.DurationS != 12 ||
		v.HasThumbnail || v.FileStatus != videos.FileNone {
		t.Fatalf("video = %+v", v)
	}
}

func TestRecordResolved(t *testing.T) {
	e := newEnv(t)
	e.video("tiktok:a")
	ctx := context.Background()
	for range 3 {
		if err := e.svc.RecordResolved(ctx, e.uid, "tiktok:a"); err != nil {
			t.Fatal(err)
		}
	}
	e.now = e.now.Add(25 * time.Hour)
	_ = e.svc.RecordResolved(ctx, e.uid, "tiktok:a")
	if got := len(e.list("", 0).Items[0].Actions); got != 2 {
		t.Fatalf("resolved entries = %d, want 2", got)
	}
}

func TestLimitsAndCleanup(t *testing.T) {
	e := newEnv(t)
	e.video("tiktok:old")
	e.action(e.uid, "tiktok:old", "sent", "X", 91*24*time.Hour)
	for i := range history.MaxActions + 5 {
		e.action(e.uid, fmt.Sprintf("tiktok:n%d", i), "resolved", "", time.Duration(i)*time.Second)
	}
	total := 0
	cursor := ""
	for {
		p := e.list(cursor, history.MaxLimit)
		total += len(p.Items)
		if p.NextCursor == "" {
			break
		}
		cursor = p.NextCursor
	}
	if total != history.MaxActions {
		t.Fatalf("items over all pages = %d, want %d", total, history.MaxActions)
	}

	e.exec(`DELETE FROM videos WHERE id = 'tiktok:old'`)
	if err := e.svc.Cleanup(context.Background()); err != nil {
		t.Fatal(err)
	}
	var n int
	_ = e.st.DB().QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM history_videos WHERE video_id = 'tiktok:old'`).Scan(&n)
	var old int
	_ = e.st.DB().QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM history WHERE video_id = 'tiktok:old'`).Scan(&old)
	if n != 0 || old != 0 {
		t.Fatalf("after cleanup: history_videos=%d history=%d", n, old)
	}
}
