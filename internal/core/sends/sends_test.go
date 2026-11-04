package sends_test

import (
	"context"
	"html"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"google.golang.org/protobuf/proto"

	linkv1 "github.com/tuskar2321/quoker/gen/quoker/link/v1"
	"github.com/tuskar2321/quoker/internal/core/queue"
	"github.com/tuskar2321/quoker/internal/core/sends"
	"github.com/tuskar2321/quoker/internal/core/store"
	"github.com/tuskar2321/quoker/internal/core/targets"
	"github.com/tuskar2321/quoker/internal/core/videos"
	"github.com/tuskar2321/quoker/internal/shared/errs"
)

type env struct {
	st       *store.Store
	q        *queue.Store
	vidSvc   *videos.Service
	tgtSvc   *targets.Service
	svc      *sends.Service
	userID   int64
	targetID int64
	videoID  string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	p := filepath.Join(t.TempDir(), "q.db")
	st, err := store.Open(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	now := time.Now
	q := queue.New(st, now)
	vidSvc := videos.New(st, q, now)
	tgtSvc := targets.New(st, now)
	svc := sends.New(st, q, vidSvc, tgtSvc, now)

	ctx := context.Background()

	// Insert a user.
	res, err := st.DB().ExecContext(ctx,
		`INSERT INTO users (role, status, tg_user_id, tg_username, tg_first_name, created_at)
		 VALUES ('user','active',12345,'testuser','Test',?)`, time.Now().UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	uid, _ := res.LastInsertId()

	// Insert a video directly.
	videoID := "tiktok:9001"
	_, err = st.DB().ExecContext(ctx,
		`INSERT INTO videos (id, platform, native_id, canonical_url, file_status, resolved_at, expires_at, last_access_at)
		 VALUES (?, 'tiktok', '9001', 'https://www.tiktok.com/@a/video/9001', 'none', ?, ?, ?)`,
		videoID, now().UnixMilli(), now().Add(24*time.Hour).UnixMilli(), now().UnixMilli())
	if err != nil {
		t.Fatal(err)
	}

	// Insert a target.
	tgt, err := tgtSvc.Upsert(ctx, targets.Target{
		OwnerUserID: uid,
		ChatID:      -100500,
		Type:        "group",
		Title:       "Test Chat",
		Status:      targets.StatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}

	return &env{
		st:       st,
		q:        q,
		vidSvc:   vidSvc,
		tgtSvc:   tgtSvc,
		svc:      svc,
		userID:   uid,
		targetID: tgt.ID,
		videoID:  videoID,
	}
}

func TestCreate_basic(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	snd, err := e.svc.Create(ctx, sends.CreateParams{
		VideoID:        e.videoID,
		TargetID:       e.targetID,
		Caption:        nil,
		SignAsUser:     false,
		IdempotencyKey: "key-001",
		CallerID:       e.userID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if snd.ID == 0 {
		t.Fatal("expected non-zero ID")
	}
	if snd.Status != sends.StatusQueued {
		t.Fatalf("status = %q, want queued", snd.Status)
	}
	if snd.VideoID != e.videoID {
		t.Fatalf("video_id = %q", snd.VideoID)
	}
	if snd.TaskID == "" {
		t.Fatal("expected task_id to be set")
	}
}

func TestCreate_idempotent(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	first, err := e.svc.Create(ctx, sends.CreateParams{
		VideoID: e.videoID, TargetID: e.targetID,
		IdempotencyKey: "key-idem", CallerID: e.userID,
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := e.svc.Create(ctx, sends.CreateParams{
		VideoID: e.videoID, TargetID: e.targetID,
		IdempotencyKey: "key-idem", CallerID: e.userID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("idempotency failed: %d vs %d", first.ID, second.ID)
	}
}

func TestCreate_videoNotFound(t *testing.T) {
	e := newEnv(t)
	_, err := e.svc.Create(context.Background(), sends.CreateParams{
		VideoID: "tiktok:nonexistent", TargetID: e.targetID,
		IdempotencyKey: "key-nf1", CallerID: e.userID,
	})
	if !errs.Is(err, errs.NotFound) {
		t.Fatalf("expected not_found, got %v", err)
	}
}

func TestCreate_targetNotFound(t *testing.T) {
	e := newEnv(t)
	_, err := e.svc.Create(context.Background(), sends.CreateParams{
		VideoID: e.videoID, TargetID: 9999,
		IdempotencyKey: "key-nf2", CallerID: e.userID,
	})
	if !errs.Is(err, errs.NotFound) {
		t.Fatalf("expected not_found, got %v", err)
	}
}

func TestCreate_targetInactive(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	// Make the target inactive.
	if err := e.tgtSvc.SetStatus(ctx, -100500, targets.StatusInactive, targets.ReasonBotRemoved); err != nil {
		t.Fatal(err)
	}

	_, err := e.svc.Create(ctx, sends.CreateParams{
		VideoID: e.videoID, TargetID: e.targetID,
		IdempotencyKey: "key-inactive", CallerID: e.userID,
	})
	if !errs.Is(err, errs.TargetInactive) {
		t.Fatalf("expected target_inactive, got %v", err)
	}
}

func TestGet_ownershipEnforced(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	// Insert a second user.
	res, err := e.st.DB().ExecContext(ctx,
		`INSERT INTO users (role, status, created_at) VALUES ('user','active',?)`,
		time.Now().UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	uid2, _ := res.LastInsertId()

	snd, err := e.svc.Create(ctx, sends.CreateParams{
		VideoID: e.videoID, TargetID: e.targetID,
		IdempotencyKey: "key-own", CallerID: e.userID,
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = e.svc.Get(ctx, uid2, snd.ID)
	if !errs.Is(err, errs.NotFound) {
		t.Fatalf("expected not_found for wrong owner, got %v", err)
	}
}

func TestCreate_captionDefaultsToURL(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	snd, err := e.svc.Create(ctx, sends.CreateParams{
		VideoID: e.videoID, TargetID: e.targetID,
		Caption:        nil, // should default to canonical_url
		IdempotencyKey: "key-cap-default", CallerID: e.userID,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Caption HTML should contain the canonical URL text.
	if snd.CaptionHTML == "" {
		t.Fatal("expected non-empty caption_html from canonical_url default")
	}
}

func TestCreate_captionEmpty(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	empty := ""
	snd, err := e.svc.Create(ctx, sends.CreateParams{
		VideoID: e.videoID, TargetID: e.targetID,
		Caption:        &empty,
		IdempotencyKey: "key-cap-empty", CallerID: e.userID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if snd.CaptionHTML != "" {
		t.Fatalf("expected empty caption_html, got %q", snd.CaptionHTML)
	}
}

func TestCreate_enqueuedTask(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	snd, err := e.svc.Create(ctx, sends.CreateParams{
		VideoID: e.videoID, TargetID: e.targetID,
		IdempotencyKey: "key-task", CallerID: e.userID,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Verify that the task exists in the queue with the right kind.
	task, err := e.q.Get(ctx, snd.TaskID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if task.Kind != queue.KindSendVideo {
		t.Fatalf("task kind = %q, want send_video", task.Kind)
	}

	// Verify payload round-trips.
	var payload linkv1.SendVideoTask
	if err := proto.Unmarshal(task.Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.VideoId != e.videoID {
		t.Fatalf("payload video_id = %q", payload.VideoId)
	}
}

func TestBuildCaptionFull_signGroup(t *testing.T) {
	vid := &videos.Video{CanonicalURL: "https://example.com/v"}
	tgt := targets.Target{Type: "group"}

	got := sends.BuildCaption(nil, vid, tgt, true, "myuser", "")
	want := "https://example.com/v\nот @myuser"
	if got != want {
		t.Fatalf("caption = %q, want %q", got, want)
	}
}

func TestBuildCaptionFull_signGroupNoUsername(t *testing.T) {
	vid := &videos.Video{CanonicalURL: "https://example.com/v"}
	tgt := targets.Target{Type: "group"}

	got := sends.BuildCaption(nil, vid, tgt, true, "", "Alice")
	want := "https://example.com/v\nот Alice"
	if got != want {
		t.Fatalf("caption = %q, want %q", got, want)
	}
}

func TestBuildCaptionFull_signChannel_ignored(t *testing.T) {
	vid := &videos.Video{CanonicalURL: "https://example.com/v"}
	tgt := targets.Target{Type: "channel"}

	got := sends.BuildCaption(nil, vid, tgt, true, "user", "Name")
	// Channels do not get user signatures.
	if got != "https://example.com/v" {
		t.Fatalf("caption = %q", got)
	}
}

func TestBuildCaptionFull_htmlEscape(t *testing.T) {
	caption := "a & b <b>bold</b>"
	vid := &videos.Video{CanonicalURL: "https://x"}
	tgt := targets.Target{Type: "private"}

	got := sends.BuildCaption(&caption, vid, tgt, false, "", "")
	want := "a &amp; b &lt;b&gt;bold&lt;/b&gt;"
	if got != want {
		t.Fatalf("caption = %q, want %q", got, want)
	}
}

func (e *env) create(t *testing.T, key string) *sends.Send {
	t.Helper()
	snd, err := e.svc.Create(context.Background(), sends.CreateParams{
		VideoID: e.videoID, TargetID: e.targetID, IdempotencyKey: key, CallerID: e.userID,
	})
	if err != nil {
		t.Fatal(err)
	}
	return snd
}

func countRows(t *testing.T, st *store.Store, query string, args ...any) int {
	t.Helper()
	var n int
	if err := st.DB().QueryRowContext(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestCreate_idempotentDoesNotEnqueueTwice(t *testing.T) {
	e := newEnv(t)
	e.create(t, "k")
	e.create(t, "k")
	if n := countRows(t, e.st, `SELECT COUNT(*) FROM tasks WHERE kind = 'send_video'`); n != 1 {
		t.Fatalf("send_video tasks = %d, want 1", n)
	}
}

func TestGet_sendingWhileLeased(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	snd := e.create(t, "k")
	if _, err := e.q.Lease(ctx, "w1"); err != nil {
		t.Fatal(err)
	}
	got, err := e.svc.Get(ctx, e.userID, snd.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != sends.StatusSending {
		t.Fatalf("status = %q, want sending", got.Status)
	}
}

func TestMarkSent(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	snd := e.create(t, "k")

	for range 2 { // the second call must be a no-op
		if err := e.svc.MarkSent(ctx, snd.TaskID, 777); err != nil {
			t.Fatal(err)
		}
	}
	got, err := e.svc.Get(ctx, e.userID, snd.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != sends.StatusSent || got.TgMessageID != 777 || got.ErrorCode != "" {
		t.Fatalf("send = %+v", got)
	}
	tgt, err := e.tgtSvc.Get(ctx, e.userID, e.targetID)
	if err != nil {
		t.Fatal(err)
	}
	if tgt.LastUsedAt.IsZero() {
		t.Fatal("target last_used_at not set")
	}
	if n := countRows(t, e.st,
		`SELECT COUNT(*) FROM history WHERE send_id = ? AND action = 'sent' AND target_title = 'Test Chat'`,
		snd.ID); n != 1 {
		t.Fatalf("history rows = %d, want 1", n)
	}
}

func TestMarkFailed(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	snd := e.create(t, "k")

	if err := e.svc.MarkFailed(ctx, snd.TaskID, "target_forbidden"); err != nil {
		t.Fatal(err)
	}
	got, err := e.svc.Get(ctx, e.userID, snd.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != sends.StatusFailed || got.ErrorCode != "target_forbidden" {
		t.Fatalf("send = %+v", got)
	}
	tgt, _ := e.tgtSvc.Get(ctx, e.userID, e.targetID)
	if !tgt.LastUsedAt.IsZero() {
		t.Fatal("failed send must not touch last_used_at")
	}
	if n := countRows(t, e.st,
		`SELECT COUNT(*) FROM history WHERE send_id = ? AND action = 'send_failed'`, snd.ID); n != 1 {
		t.Fatalf("history rows = %d, want 1", n)
	}
	// A late success after the final failure does not resurrect the send.
	if err := e.svc.MarkSent(ctx, snd.TaskID, 1); err != nil {
		t.Fatal(err)
	}
	if got, _ := e.svc.Get(ctx, e.userID, snd.ID); got.Status != sends.StatusFailed {
		t.Fatalf("status = %q, want failed", got.Status)
	}
}

func TestMarkSent_unknownTask(t *testing.T) {
	e := newEnv(t)
	if err := e.svc.MarkSent(context.Background(), "NOPE", 1); !errs.Is(err, errs.NotFound) {
		t.Fatalf("err = %v, want not_found", err)
	}
}

func TestBuildCaption_truncation(t *testing.T) {
	vid := &videos.Video{CanonicalURL: "https://x"}
	group := targets.Target{Type: "group"}
	long := strings.Repeat("я", 2000)
	amps := strings.Repeat("&", 2000)

	tests := []struct {
		name      string
		caption   string
		sign      bool
		wantRunes int    // visible characters after unescaping
		wantTail  string // expected suffix
	}{
		{"plain", long, false, 1024, "я"},
		{"with signature", long, true, 1024, "\nот @user"},
		{"entities not split", amps, false, 1024 * len("&amp;"), "&amp;"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sends.BuildCaption(&tt.caption, vid, group, tt.sign, "user", "")
			if !strings.HasSuffix(got, tt.wantTail) {
				t.Fatalf("suffix of %q, want %q", got[len(got)-20:], tt.wantTail)
			}
			n := utf8.RuneCountInString(html.UnescapeString(got))
			if tt.name == "entities not split" {
				n = len(got)
			}
			if n != tt.wantRunes {
				t.Fatalf("length = %d, want %d", n, tt.wantRunes)
			}
		})
	}
}

// TestBuildCaption_emojiCountsTwice: Telegram measures captions in UTF-16,
// where an emoji outside the BMP is two units (REVIEW M14).
func TestBuildCaption_emojiCountsTwice(t *testing.T) {
	vid := &videos.Video{CanonicalURL: "https://x"}
	caption := strings.Repeat("😀", 600) // 600 characters, 1200 units
	got := sends.BuildCaption(&caption, vid, targets.Target{Type: "channel"}, false, "", "")
	if n := sends.CaptionLen(got); n != 1024 {
		t.Fatalf("caption is %d UTF-16 units, want 1024", n)
	}
	if !utf8.ValidString(got) {
		t.Fatal("truncation split a character")
	}
	if utf8.RuneCountInString(got) != 512 {
		t.Fatalf("kept %d emoji, want 512", utf8.RuneCountInString(got))
	}
}
