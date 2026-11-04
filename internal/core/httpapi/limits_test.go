package httpapi

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/tuskar2321/quoker/internal/core/accounts"
)

// TestResolveRateLimit: 30 resolves an hour per user (REVIEW M8).
func TestResolveRateLimit(t *testing.T) {
	e := newEnv(t)
	tok, _ := e.user(accounts.RoleUser)
	other, _ := e.user(accounts.RoleUser)
	e.addVideo("tiktok:1", "tiktok") // a cache hit answers at once
	resolve := func(token string) resp {
		return e.do(req{method: http.MethodPost, path: "/v1/videos/resolve", token: token,
			body: map[string]string{"url": "https://www.tiktok.com/@a/video/1"}})
	}
	for i := range resolvesPerHour {
		if r := resolve(tok); r.status != http.StatusOK {
			t.Fatalf("resolve %d: %d %s", i, r.status, r.raw)
		}
	}
	r := resolve(tok)
	if r.errCode() != "rate_limited" || r.header.Get("Retry-After") == "" {
		t.Fatalf("over the limit: %d %s", r.status, r.raw)
	}
	if r := resolve(other); r.status != http.StatusOK {
		t.Fatalf("another user is limited too: %d", r.status)
	}
}

// TestRefreshThrottled: one re-check per user per minute (REVIEW M9).
func TestRefreshThrottled(t *testing.T) {
	e := newEnv(t)
	tok, uid := e.user(accounts.RoleUser)
	if _, err := e.st.DB().ExecContext(context.Background(), `UPDATE users SET tg_user_id = 5 WHERE id = ?`, uid); err != nil {
		t.Fatal(err)
	}
	refresh := func() resp { return e.do(req{method: http.MethodPost, path: "/v1/targets/refresh", token: tok}) }
	if r := refresh(); r.status != http.StatusAccepted {
		t.Fatalf("first: %d %s", r.status, r.raw)
	}
	deadline := time.Now().Add(2 * time.Second)
	for !e.refreshIdle(uid) {
		if time.Now().After(deadline) {
			t.Fatal("re-check never finished")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if r := refresh(); r.errCode() != "rate_limited" {
		t.Fatalf("second within a minute: %d %s", r.status, r.raw)
	}
	e.now = e.now.Add(61 * time.Second)
	if r := refresh(); r.status != http.StatusAccepted {
		t.Fatalf("after a minute: %d %s", r.status, r.raw)
	}
}

func TestInFlight(t *testing.T) {
	f := &inFlight{running: map[int64]bool{}}
	if !f.start(1) || f.start(1) || !f.start(2) {
		t.Fatal("start must admit one run per user")
	}
	f.done(1)
	if !f.start(1) {
		t.Fatal("slot not released")
	}
}
