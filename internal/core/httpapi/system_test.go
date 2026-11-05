package httpapi

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/tuskar2321/quoker/internal/core/accounts"
	"github.com/tuskar2321/quoker/internal/core/queue"
	"github.com/tuskar2321/quoker/internal/core/targets"
)

func TestAdminSystemEndpoints(t *testing.T) {
	e := newEnv(t)
	adminTok, _ := e.user(accounts.RoleAdmin)
	userTok, _ := e.user(accounts.RoleUser)
	fp := strings.Repeat("ab", 32)

	if r := e.do(req{method: http.MethodGet, path: "/v1/admin/status", token: userTok}); r.status != http.StatusForbidden {
		t.Fatalf("user status: %d", r.status)
	}
	st := e.do(req{method: http.MethodGet, path: "/v1/admin/status", token: adminTok})
	queue, _ := st.body["queue"].(map[string]any)
	versions, _ := st.body["versions"].(map[string]any)
	if st.status != http.StatusOK || queue["queued"] != float64(0) || versions["core"] != "test" || st.body["errors_24h"] == nil {
		t.Fatalf("status = %s", st.raw)
	}

	if r := e.do(req{method: http.MethodPost, path: "/v1/admin/workers", token: adminTok,
		body: map[string]string{"id": "w-1", "fingerprint": fp}}); r.status != http.StatusCreated {
		t.Fatalf("allow: %d %s", r.status, r.raw)
	}
	if r := e.do(req{method: http.MethodPost, path: "/v1/admin/workers", token: adminTok,
		body: map[string]string{"id": "w-2", "fingerprint": "nope"}}); r.errCode() != "invalid_request" {
		t.Fatalf("bad fingerprint: %s", r.raw)
	}
	list := e.do(req{method: http.MethodGet, path: "/v1/admin/workers", token: adminTok})
	items, _ := list.body["items"].([]any)
	if len(items) == 1 && items[0].(map[string]any)["cert_not_after"] != nil {
		t.Fatalf("cert_not_after must be null before the first connect: %s", list.raw)
	}
	if _, err := e.st.DB().ExecContext(context.Background(),
		`UPDATE workers SET cert_not_after = ? WHERE id = 'w-1'`, time.Date(2027, 1, 2, 3, 4, 5, 0, time.UTC).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if got := e.do(req{method: http.MethodGet, path: "/v1/admin/workers", token: adminTok}); !strings.Contains(got.raw, `"cert_not_after":"2027-01-02T03:04:05Z"`) {
		t.Fatalf("cert_not_after missing: %s", got.raw)
	}
	if len(items) != 1 || items[0].(map[string]any)["connected"] != false || items[0].(map[string]any)["version"] != nil {
		t.Fatalf("workers = %s", list.raw)
	}
	if r := e.do(req{method: http.MethodDelete, path: "/v1/admin/workers/w-1", token: adminTok}); r.status != http.StatusNoContent {
		t.Fatalf("revoke: %d", r.status)
	}
	if r := e.do(req{method: http.MethodDelete, path: "/v1/admin/workers/ghost", token: adminTok}); r.status != http.StatusNotFound {
		t.Fatalf("revoke unknown: %d", r.status)
	}

	if r := e.do(req{method: http.MethodPut, path: "/v1/admin/instagram-cookies", token: adminTok,
		body: ".instagram.com\tTRUE\t/\tTRUE\t0\tsessionid\tx"}); r.status != http.StatusNoContent {
		t.Fatalf("cookies: %d %s", r.status, r.raw)
	}
	if r := e.do(req{method: http.MethodPut, path: "/v1/admin/instagram-cookies", token: adminTok,
		body: "not cookies"}); r.errCode() != "invalid_request" {
		t.Fatalf("bad cookies: %s", r.raw)
	}
}

func TestRefreshTargets(t *testing.T) {
	e := newEnv(t)
	tok, uid := e.user(accounts.RoleUser)
	if r := e.do(req{method: http.MethodPost, path: "/v1/targets/refresh", token: tok}); r.errCode() != "telegram_not_linked" {
		t.Fatalf("unlinked: %d %s", r.status, r.raw)
	}
	if _, err := e.st.DB().ExecContext(context.Background(), `UPDATE users SET tg_user_id = 5 WHERE id = ?`, uid); err != nil {
		t.Fatal(err)
	}
	if r := e.do(req{method: http.MethodPost, path: "/v1/targets/refresh", token: tok}); r.status != http.StatusAccepted {
		t.Fatalf("linked: %d %s", r.status, r.raw)
	}
}

// TestContractShapes: nullable target fields are present as null and only
// resolve/download tasks are visible (REVIEW K1, K3).
func TestContractShapes(t *testing.T) {
	e := newEnv(t)
	tok, uid := e.user(accounts.RoleUser)
	ctx := context.Background()
	if _, err := e.targets.Upsert(ctx, targets.Target{OwnerUserID: uid, ChatID: -1, Type: "group", Title: "G", Status: targets.StatusActive}); err != nil {
		t.Fatal(err)
	}
	r := e.do(req{method: http.MethodGet, path: "/v1/targets", token: tok})
	if !strings.Contains(r.raw, `"inactive_reason":null`) || !strings.Contains(r.raw, `"last_used_at":null`) {
		t.Fatalf("targets = %s", r.raw)
	}
	id, _ := e.queue.Enqueue(ctx, queue.EnqueueParams{Kind: queue.KindBotCall, Payload: []byte{}})
	if r := e.do(req{method: http.MethodGet, path: "/v1/tasks/" + id, token: tok}); r.status != http.StatusNotFound {
		t.Fatalf("bot_call task visible: %d %s", r.status, r.raw)
	}
}
