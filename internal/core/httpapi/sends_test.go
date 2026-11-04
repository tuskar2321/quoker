package httpapi

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/tuskar2321/quoker/internal/core/accounts"
	"github.com/tuskar2321/quoker/internal/core/targets"
)

// sendFixture creates a user (optionally with Telegram linked), a cached video
// and an active target, and returns the token and target id.
func (e *env) sendFixture(linked bool) (string, int64) {
	e.t.Helper()
	ctx := context.Background()
	tok, uid := e.user(accounts.RoleUser)
	if linked {
		if _, err := e.st.DB().ExecContext(ctx,
			`UPDATE users SET tg_user_id = ?, tg_username = 'max' WHERE id = ?`, 1000+uid, uid); err != nil {
			e.t.Fatal(err)
		}
	}
	ms := e.now.UnixMilli()
	if _, err := e.st.DB().ExecContext(ctx,
		`INSERT OR IGNORE INTO videos (id, platform, native_id, canonical_url, file_status, resolved_at, expires_at, last_access_at)
		 VALUES ('tiktok:1','tiktok','1','https://www.tiktok.com/@a/video/1','none',?,?,?)`,
		ms, ms+86_400_000, ms); err != nil {
		e.t.Fatal(err)
	}
	tgt, err := e.targets.Upsert(ctx, targets.Target{
		OwnerUserID: uid, ChatID: -100, Type: "group", Title: "Друзья", Status: targets.StatusActive,
	})
	if err != nil {
		e.t.Fatal(err)
	}
	return tok, tgt.ID
}

func TestCreateSend(t *testing.T) {
	long := strings.Repeat("я", 1025)
	tests := []struct {
		name     string
		linked   bool
		key      string
		body     func(targetID int64) map[string]any
		status   int
		wantCode string
	}{
		{"ok", true, "k1", func(id int64) map[string]any {
			return map[string]any{"video_id": "tiktok:1", "target_id": id}
		}, http.StatusAccepted, ""},
		{"no idempotency key", true, "", func(id int64) map[string]any {
			return map[string]any{"video_id": "tiktok:1", "target_id": id}
		}, http.StatusBadRequest, "invalid_request"},
		{"caption too long", true, "k2", func(id int64) map[string]any {
			return map[string]any{"video_id": "tiktok:1", "target_id": id, "caption": long}
		}, http.StatusBadRequest, "invalid_request"},
		{"emoji caption over the limit in UTF-16", true, "k5", func(id int64) map[string]any {
			return map[string]any{"video_id": "tiktok:1", "target_id": id, "caption": strings.Repeat("😀", 600)}
		}, http.StatusBadRequest, "invalid_request"},
		{"telegram not linked", false, "k3", func(id int64) map[string]any {
			return map[string]any{"video_id": "tiktok:1", "target_id": id}
		}, http.StatusConflict, "telegram_not_linked"},
		{"unknown video", true, "k4", func(id int64) map[string]any {
			return map[string]any{"video_id": "tiktok:404", "target_id": id}
		}, http.StatusNotFound, "not_found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			tok, targetID := e.sendFixture(tt.linked)
			r := e.do(req{method: http.MethodPost, path: "/v1/sends", token: tok,
				body: tt.body(targetID), header: map[string]string{"Idempotency-Key": tt.key}})
			if r.status != tt.status || r.errCode() != tt.wantCode {
				t.Fatalf("status = %d code = %q, want %d %q; body %s", r.status, r.errCode(), tt.status, tt.wantCode, r.raw)
			}
		})
	}
}

func TestCreateSend_idempotentAndGet(t *testing.T) {
	e := newEnv(t)
	tok, targetID := e.sendFixture(true)
	post := func() resp {
		return e.do(req{method: http.MethodPost, path: "/v1/sends", token: tok,
			body:   map[string]any{"video_id": "tiktok:1", "target_id": targetID, "sign_as_user": true},
			header: map[string]string{"Idempotency-Key": "same"}})
	}
	first, second := post(), post()
	id1 := first.body["send"].(map[string]any)["id"]
	id2 := second.body["send"].(map[string]any)["id"]
	if first.status != http.StatusAccepted || id1 != id2 {
		t.Fatalf("first %d %v, second %d %v", first.status, id1, second.status, id2)
	}

	id := strconv.FormatFloat(id1.(float64), 'f', 0, 64)
	got := e.do(req{method: http.MethodGet, path: "/v1/sends/" + id, token: tok})
	send, _ := got.body["send"].(map[string]any)
	if got.status != http.StatusOK || send["status"] != "queued" || send["error"] != nil {
		t.Fatalf("get: %d %s", got.status, got.raw)
	}

	other, _ := e.user(accounts.RoleUser)
	if r := e.do(req{method: http.MethodGet, path: "/v1/sends/" + id, token: other}); r.status != http.StatusNotFound {
		t.Fatalf("foreign send: status %d", r.status)
	}
}

func TestCreateSend_rateLimited(t *testing.T) {
	e := newEnv(t)
	tok, targetID := e.sendFixture(true)
	var last resp
	for i := range sendsPerHour + 1 {
		last = e.do(req{method: http.MethodPost, path: "/v1/sends", token: tok,
			body:   map[string]any{"video_id": "tiktok:1", "target_id": targetID},
			header: map[string]string{"Idempotency-Key": "k" + strconv.Itoa(i)}})
	}
	if last.status != http.StatusTooManyRequests || last.header.Get("Retry-After") == "" {
		t.Fatalf("status = %d, Retry-After = %q", last.status, last.header.Get("Retry-After"))
	}
}
