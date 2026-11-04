package admin_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	linkv1 "github.com/tuskar2321/quoker/gen/quoker/link/v1"
	"github.com/tuskar2321/quoker/internal/core/admin"
	"github.com/tuskar2321/quoker/internal/core/botapi"
	"github.com/tuskar2321/quoker/internal/core/queue"
	"github.com/tuskar2321/quoker/internal/core/settings"
	"github.com/tuskar2321/quoker/internal/core/store"
	"github.com/tuskar2321/quoker/internal/shared/errs"
)

type fakeLink struct {
	connected  []string
	broadcasts int
	shutdown   []string
}

func (f *fakeLink) ConnectedWorkers() []string            { return f.connected }
func (f *fakeLink) BroadcastConfig(context.Context) error { f.broadcasts++; return nil }
func (f *fakeLink) Shutdown(_ context.Context, id, _ string) error {
	f.shutdown = append(f.shutdown, id)
	return nil
}

type env struct {
	t    *testing.T
	st   *store.Store
	set  *settings.Store
	q    *queue.Store
	link *fakeLink
	svc  *admin.Service
	now  time.Time
}

func newEnv(t *testing.T) *env {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "q.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	// 09:30 MSK.
	e := &env{t: t, st: st, link: &fakeLink{}, now: time.Date(2026, 11, 4, 6, 30, 0, 0, time.UTC)}
	clock := func() time.Time { return e.now }
	e.set = settings.New(st.DB(), clock)
	e.q = queue.New(st, clock)
	e.svc = admin.New(admin.Deps{
		Store: st, Settings: e.set, Link: e.link, Bot: botapi.New(e.q),
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Now: clock,
	})
	if err := e.set.Set(context.Background(), settings.AdminChatID, "500"); err != nil {
		t.Fatal(err)
	}
	return e
}

// messages returns the texts queued for the admin chat.
func (e *env) messages() []string {
	e.t.Helper()
	rows, err := e.st.DB().Query(`SELECT payload FROM tasks WHERE kind = 'bot_call' ORDER BY created_at`)
	if err != nil {
		e.t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var payload []byte
		_ = rows.Scan(&payload)
		var bc linkv1.BotCallTask
		_ = proto.Unmarshal(payload, &bc)
		var p struct {
			ChatID int64  `json:"chat_id"`
			Text   string `json:"text"`
		}
		_ = json.Unmarshal([]byte(bc.ParamsJson), &p)
		if bc.Method == "sendMessage" && p.ChatID == 500 {
			out = append(out, p.Text)
		}
	}
	return out
}

// finished records a resolve task for url that ended with code ("" = done).
func (e *env) finished(url, code string) {
	e.t.Helper()
	payload, _ := proto.Marshal(&linkv1.ResolveTask{Url: url})
	status := "done"
	if code != "" {
		status = "failed"
	}
	ms := e.now.UnixMilli()
	if _, err := e.st.DB().Exec(
		`INSERT INTO tasks (id, kind, payload, status, priority, max_attempts, error_code, not_before, created_at, updated_at)
		 VALUES (lower(hex(randomblob(8))), 'resolve', ?, ?, 20, 3, NULLIF(?, ''), ?, ?, ?)`,
		payload, status, code, ms, ms, ms); err != nil {
		e.t.Fatal(err)
	}
}

func TestWorkers(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	fp := strings.Repeat("Ab:", 31) + "Ab"
	if err := e.svc.AllowWorker(ctx, "w-1", fp); err != nil {
		t.Fatal(err)
	}
	for _, bad := range [][2]string{{"w 1", fp}, {"w-2", "short"}, {"w-2", fp}} {
		if err := e.svc.AllowWorker(ctx, bad[0], bad[1]); !errs.Is(err, errs.InvalidRequest) {
			t.Fatalf("AllowWorker(%q, %q) = %v", bad[0], bad[1], err)
		}
	}
	e.link.connected = []string{"w-1"}
	_ = e.set.Set(ctx, settings.PollerWorkerID, "w-1")
	ws, err := e.svc.Workers(ctx)
	if err != nil || len(ws) != 1 || !ws[0].Connected || !ws[0].IsPoller || ws[0].Fingerprint != strings.Repeat("ab", 32) {
		t.Fatalf("workers = %+v, %v", ws, err)
	}
	if err := e.svc.RevokeWorker(ctx, "w-1"); err != nil || len(e.link.shutdown) != 1 {
		t.Fatalf("revoke: %v, shutdowns %v", err, e.link.shutdown)
	}
	if err := e.svc.RevokeWorker(ctx, "nope"); !errs.Is(err, errs.NotFound) {
		t.Fatalf("revoke unknown: %v", err)
	}
}

func TestSetCookies(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	for _, bad := range []string{"", "# Netscape HTTP Cookie File\n.tiktok.com\tTRUE", strings.Repeat("x", admin.MaxCookiesBytes+1)} {
		if err := e.svc.SetCookies(ctx, bad); !errs.Is(err, errs.InvalidRequest) {
			t.Fatalf("SetCookies(%.20q) = %v", bad, err)
		}
	}
	if err := e.svc.SetCookies(ctx, ".instagram.com\tTRUE\t/\tTRUE\t0\tsessionid\tx\n"); err != nil {
		t.Fatal(err)
	}
	if v, _, _ := e.set.Get(ctx, settings.InstagramCookies); !strings.HasPrefix(v, ".instagram.com") || e.link.broadcasts != 1 {
		t.Fatalf("stored %q, broadcasts %d", v, e.link.broadcasts)
	}
}

func TestNoWorkersAlert(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	_ = e.svc.Check(ctx)
	e.now = e.now.Add(4 * time.Minute)
	_ = e.svc.Check(ctx)
	if len(e.messages()) != 0 {
		t.Fatalf("alert too early: %v", e.messages())
	}
	e.now = e.now.Add(2 * time.Minute)
	_ = e.svc.Check(ctx)
	_ = e.svc.Check(ctx)
	e.link.connected = []string{"w-1"}
	_ = e.svc.Check(ctx)
	got := e.messages()
	if len(got) != 2 || got[0] != "⚠️ Нет подключённых воркеров 5 минут" || got[1] != "✅ Воркер снова на связи" {
		t.Fatalf("messages = %q", got)
	}
}

func TestErrorRatioAlert(t *testing.T) {
	e := newEnv(t)
	e.link.connected = []string{"w-1"}
	ctx := context.Background()
	for i := range 10 {
		code := ""
		if i < 4 {
			code = "login_required"
		}
		e.finished("https://www.instagram.com/reel/x/", code)
		e.finished("https://www.tiktok.com/@a/video/1", "")
	}
	_ = e.svc.Check(ctx)
	_ = e.svc.Check(ctx) // no repeat within the hour
	got := e.messages()
	if len(got) != 2 || got[0] != "⚠️ Instagram: 4 из 10 запросов с ошибкой за час (login_required)" ||
		!strings.Contains(got[1], "обновите cookies") {
		t.Fatalf("messages = %q", got)
	}
	st, err := e.svc.Status(ctx)
	if err != nil || st.Errors24h["instagram"] != 4 || st.Errors24h["tiktok"] != 0 {
		t.Fatalf("status = %+v, %v", st, err)
	}
}

func TestDailySummary(t *testing.T) {
	e := newEnv(t)
	e.link.connected = []string{"w-1"}
	ctx := context.Background()
	_ = e.svc.Check(ctx) // 09:30 MSK
	e.now = e.now.Add(40 * time.Minute)
	_ = e.svc.Check(ctx) // 10:10 MSK
	e.now = e.now.Add(10 * time.Minute)
	_ = e.svc.Check(ctx) // still 10:xx — sent already today
	got := e.messages()
	if len(got) != 1 || !strings.HasPrefix(got[0], "📊 Сводка за сутки") {
		t.Fatalf("messages = %q", got)
	}
}
