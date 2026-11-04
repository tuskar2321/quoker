package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/tuskar2321/quoker/internal/core/accounts"
	"github.com/tuskar2321/quoker/internal/core/admin"
	"github.com/tuskar2321/quoker/internal/core/files"
	"github.com/tuskar2321/quoker/internal/core/history"
	"github.com/tuskar2321/quoker/internal/core/queue"
	"github.com/tuskar2321/quoker/internal/core/releases"
	"github.com/tuskar2321/quoker/internal/core/sends"
	"github.com/tuskar2321/quoker/internal/core/settings"
	"github.com/tuskar2321/quoker/internal/core/store"
	"github.com/tuskar2321/quoker/internal/core/targets"
	"github.com/tuskar2321/quoker/internal/core/videos"
)

const deviceID = "3f0c2b9e-8a41-4c7d-9b1e-2a6f5d4c3b21"

type env struct {
	t        *testing.T
	srv      *httptest.Server
	st       *store.Store
	acc      *accounts.Service
	targets  *targets.Service
	videos   *videos.Service
	files    *files.Store
	queue    *queue.Store
	releases *releases.Store
	settings *settings.Store
	now      time.Time
}

func newEnv(t *testing.T) *env {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "q.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	e := &env{t: t, st: st, now: time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)}
	clock := func() time.Time { return e.now }
	e.acc = accounts.New(st, clock, nil)
	e.settings = settings.New(st.DB(), clock)
	e.targets = targets.New(st, clock)
	q := queue.New(st, clock)
	vids := videos.New(st, q, clock)
	e.videos, e.queue = vids, q
	e.releases = releases.New(st, filepath.Join(t.TempDir(), "releases"), clock)
	if e.files, err = files.New(st, t.TempDir(), 1<<30, clock, nil); err != nil {
		t.Fatal(err)
	}
	h := New(Deps{
		Accounts: e.acc,
		Videos:   vids,
		Targets:  e.targets,
		Sends:    sends.New(st, q, vids, e.targets, clock),
		Files:    e.files,
		History:  history.New(st, clock),
		Releases: e.releases,
		Store:    st,
		Version:  "test",
		Admin: admin.New(admin.Deps{
			Store: st, Settings: e.settings, Link: noLink{}, Files: e.files, Now: clock,
		}),
		Settings: e.settings,
		Ready:    st.Ready,
		Log:      slog.New(slog.NewJSONHandler(io.Discard, nil)),
		Now:      clock,
	})
	e.srv = httptest.NewServer(h)
	t.Cleanup(e.srv.Close)
	return e
}

type resp struct {
	status int
	header http.Header
	body   map[string]any
	raw    string
}

func (r resp) errCode() string {
	e, _ := r.body["error"].(map[string]any)
	s, _ := e["code"].(string)
	return s
}

type req struct {
	method, path, token string
	body                any               // marshalled to JSON unless string
	header              map[string]string // extra headers
}

func (e *env) do(q req) resp {
	e.t.Helper()
	var body io.Reader
	switch b := q.body.(type) {
	case nil:
	case string:
		body = strings.NewReader(b)
	default:
		raw, err := json.Marshal(b)
		if err != nil {
			e.t.Fatal(err)
		}
		body = bytes.NewReader(raw)
	}
	r, err := http.NewRequestWithContext(context.Background(), q.method, e.srv.URL+q.path, body)
	if err != nil {
		e.t.Fatal(err)
	}
	if q.token != "" {
		r.Header.Set("Authorization", "Bearer "+q.token)
	}
	for k, v := range q.header {
		r.Header.Set(k, v)
	}
	res, err := e.srv.Client().Do(r)
	if err != nil {
		e.t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	raw, _ := io.ReadAll(res.Body)
	out := resp{status: res.StatusCode, header: res.Header, raw: string(raw)}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &out.body)
	}
	return out
}

// user registers a new account with the given role and returns its token and id.
func (e *env) user(role accounts.Role) (string, int64) {
	e.t.Helper()
	code, _, err := e.acc.CreateInvite(context.Background(), 0, role, accounts.DefaultInviteTTL)
	if err != nil {
		e.t.Fatal(err)
	}
	tok, u, err := e.acc.Register(context.Background(), accounts.Registration{InviteCode: code, DeviceID: deviceID, DeviceName: "Pixel"})
	if err != nil {
		e.t.Fatal(err)
	}
	return tok, u.ID
}

func expect(t *testing.T, r resp, status int, code string) {
	t.Helper()
	if r.status != status || r.errCode() != code {
		t.Fatalf("got %d %q, want %d %q; body: %s", r.status, r.errCode(), status, code, r.raw)
	}
}

func TestRegisterAndMe(t *testing.T) {
	e := newEnv(t)
	code, _, err := e.acc.CreateInvite(context.Background(), 0, accounts.RoleUser, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	r := e.do(req{method: "POST", path: "/v1/auth/register", body: map[string]string{
		"invite_code": code, "device_id": deviceID, "device_name": "Pixel 8",
	}, header: map[string]string{"X-Quoker-Version-Code": "5"}})
	expect(t, r, http.StatusCreated, "")
	token, _ := r.body["session_token"].(string)
	user, _ := r.body["user"].(map[string]any)
	if token == "" || user["role"] != "user" {
		t.Fatalf("register body: %s", r.raw)
	}

	r = e.do(req{method: "GET", path: "/v1/me", token: token})
	expect(t, r, http.StatusOK, "")
	tg, _ := r.body["telegram"].(map[string]any)
	if r.body["id"] != user["id"] || tg["linked"] != false || tg["username"] != nil {
		t.Fatalf("me body: %s", r.raw)
	}
	if r.header.Get("X-Request-Id") == "" {
		t.Fatal("missing X-Request-Id")
	}
}

func TestRegisterErrors(t *testing.T) {
	e := newEnv(t)
	tests := []struct {
		name   string
		body   any
		status int
		code   string
	}{
		{"not json", "{", http.StatusBadRequest, "invalid_request"},
		{"trailing data", `{"invite_code":"AAAA-BBBB"} {}`, http.StatusBadRequest, "invalid_request"},
		{"bad device id", map[string]string{"invite_code": "AAAA-BBBB", "device_id": "x", "device_name": "n"}, http.StatusBadRequest, "invalid_request"},
		{"empty device name", map[string]string{"invite_code": "AAAA-BBBB", "device_id": deviceID, "device_name": " "}, http.StatusBadRequest, "invalid_request"},
		{"long device name", map[string]string{"invite_code": "AAAA-BBBB", "device_id": deviceID, "device_name": strings.Repeat("я", 65)}, http.StatusBadRequest, "invalid_request"},
		{"unknown invite", map[string]string{"invite_code": "AAAA-BBBB", "device_id": deviceID, "device_name": "n"}, http.StatusBadRequest, "invalid_invite"},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Distinct IPs so the registration limit does not interfere.
			r := e.do(req{method: "POST", path: "/v1/auth/register", body: tt.body,
				header: map[string]string{"X-Forwarded-For": "10.0.0." + strconv.Itoa(i)}})
			expect(t, r, tt.status, tt.code)
		})
	}

	r := e.do(req{method: "POST", path: "/v1/auth/register",
		body: map[string]string{"invite_code": "AAAA-BBBB", "device_id": deviceID, "device_name": "n"}})
	errObj, _ := r.body["error"].(map[string]any)
	if errObj["message"] != "Код не подошёл" || errObj["retryable"] != false {
		t.Fatalf("error body: %s", r.raw)
	}
}

func TestRegisterRateLimit(t *testing.T) {
	e := newEnv(t)
	attempt := func(ip string) resp {
		return e.do(req{method: "POST", path: "/v1/auth/register",
			body:   map[string]string{"invite_code": "AAAA-BBBB", "device_id": deviceID, "device_name": "n"},
			header: map[string]string{"X-Forwarded-For": ip + ", 172.16.0.1"}})
	}
	for range registerPerHour {
		expect(t, attempt("203.0.113.7"), http.StatusBadRequest, "invalid_invite")
	}
	r := attempt("203.0.113.7")
	expect(t, r, http.StatusTooManyRequests, "rate_limited")
	if r.header.Get("Retry-After") != "3600" {
		t.Fatalf("Retry-After = %q", r.header.Get("Retry-After"))
	}
	expect(t, attempt("203.0.113.8"), http.StatusBadRequest, "invalid_invite")
}

func TestAuthentication(t *testing.T) {
	e := newEnv(t)
	adminTok, _ := e.newAdmin()
	userTok, userID := e.user(accounts.RoleUser)

	expect(t, e.do(req{method: "GET", path: "/v1/me"}), http.StatusUnauthorized, "unauthorized")
	expect(t, e.do(req{method: "GET", path: "/v1/me", token: "garbage"}), http.StatusUnauthorized, "unauthorized")
	expect(t, e.do(req{method: "GET", path: "/v1/me", header: map[string]string{"Authorization": "Basic x"}}),
		http.StatusUnauthorized, "unauthorized")

	expect(t, e.do(req{method: "POST", path: "/v1/admin/users/" + strconv.FormatInt(userID, 10) + "/block", token: adminTok}),
		http.StatusNoContent, "")
	expect(t, e.do(req{method: "GET", path: "/v1/me", token: userTok}), http.StatusForbidden, "user_blocked")
	expect(t, e.do(req{method: "POST", path: "/v1/admin/users/" + strconv.FormatInt(userID, 10) + "/unblock", token: adminTok}),
		http.StatusNoContent, "")
	expect(t, e.do(req{method: "GET", path: "/v1/me", token: userTok}), http.StatusOK, "")

	expect(t, e.do(req{method: "POST", path: "/v1/auth/logout", token: userTok}), http.StatusNoContent, "")
	expect(t, e.do(req{method: "GET", path: "/v1/me", token: userTok}), http.StatusUnauthorized, "unauthorized")
}

func TestUpgradeRequired(t *testing.T) {
	e := newEnv(t)
	tok, _ := e.user(accounts.RoleUser)
	if err := e.settings.Set(context.Background(), settings.MinSupportedVersionCode, "10"); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		version string
		status  int
		code    string
	}{
		{"9", http.StatusUpgradeRequired, "upgrade_required"},
		{"10", http.StatusOK, ""},
		{"", http.StatusOK, ""},
		{"abc", http.StatusBadRequest, "invalid_request"},
	}
	for _, tt := range tests {
		t.Run("v="+tt.version, func(t *testing.T) {
			h := map[string]string{}
			if tt.version != "" {
				h["X-Quoker-Version-Code"] = tt.version
			}
			expect(t, e.do(req{method: "GET", path: "/v1/me", token: tok, header: h}), tt.status, tt.code)
		})
	}
}

func TestAdminRequiresRole(t *testing.T) {
	e := newEnv(t)
	tok, _ := e.user(accounts.RoleUser)
	for _, q := range []req{
		{method: "GET", path: "/v1/admin/users"},
		{method: "GET", path: "/v1/admin/invites"},
		{method: "POST", path: "/v1/admin/invites", body: map[string]any{}},
		{method: "POST", path: "/v1/admin/users/1/block"},
	} {
		q.token = tok
		expect(t, e.do(q), http.StatusForbidden, "forbidden")
	}
}

var inviteCodeRe = regexp.MustCompile(`^[A-Z0-9]{4}-[A-Z0-9]{4}$`)

func TestAdminInvites(t *testing.T) {
	e := newEnv(t)
	tok, _ := e.newAdmin()

	r := e.do(req{method: "POST", path: "/v1/admin/invites", token: tok}) // empty body: defaults
	expect(t, r, http.StatusCreated, "")
	code, _ := r.body["code"].(string)
	if !inviteCodeRe.MatchString(code) || r.body["expires_at"] != "2026-11-10T12:00:00Z" {
		t.Fatalf("create body: %s", r.raw)
	}
	r = e.do(req{method: "POST", path: "/v1/admin/invites", token: tok, body: map[string]any{"role": "admin", "expires_in_days": 30}})
	expect(t, r, http.StatusCreated, "")

	for _, body := range []any{
		map[string]any{"expires_in_days": 0},
		map[string]any{"expires_in_days": 31},
		map[string]any{"role": "root"},
	} {
		expect(t, e.do(req{method: "POST", path: "/v1/admin/invites", token: tok, body: body}),
			http.StatusBadRequest, "invalid_request")
	}

	r = e.do(req{method: "GET", path: "/v1/admin/invites", token: tok})
	expect(t, r, http.StatusOK, "")
	items, _ := r.body["items"].([]any)
	// The admin's own (used) bootstrap invite plus the two new ones.
	if len(items) != 3 {
		t.Fatalf("invites: %s", r.raw)
	}
	var target map[string]any
	for _, it := range items {
		m := it.(map[string]any)
		if m["code_prefix"] == code[:4] {
			target = m
		}
	}
	if target == nil || target["role"] != "user" || target["used_by"] != nil {
		t.Fatalf("invite for %s not listed: %s", code, r.raw)
	}

	id := strconv.FormatFloat(target["id"].(float64), 'f', 0, 64)
	expect(t, e.do(req{method: "DELETE", path: "/v1/admin/invites/" + id, token: tok}), http.StatusNoContent, "")
	expect(t, e.do(req{method: "DELETE", path: "/v1/admin/invites/999", token: tok}), http.StatusNotFound, "not_found")
	expect(t, e.do(req{method: "DELETE", path: "/v1/admin/invites/abc", token: tok}), http.StatusNotFound, "not_found")

	r = e.do(req{method: "POST", path: "/v1/auth/register",
		body: map[string]string{"invite_code": code, "device_id": deviceID, "device_name": "n"}})
	expect(t, r, http.StatusBadRequest, "invalid_invite")
}

func TestAdminUsers(t *testing.T) {
	e := newEnv(t)
	tok, adminID := e.newAdmin()
	_, userID := e.user(accounts.RoleUser)

	r := e.do(req{method: "GET", path: "/v1/admin/users", token: tok})
	expect(t, r, http.StatusOK, "")
	items, _ := r.body["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("users: %s", r.raw)
	}
	u := items[1].(map[string]any)
	if u["role"] != "user" || u["status"] != "active" || u["targets_count"] != float64(0) ||
		u["telegram_username"] != nil || u["telegram_first_name"] != nil || u["created_at"] != "2026-11-03T12:00:00Z" {
		t.Fatalf("user item: %v", u)
	}

	self := "/v1/admin/users/" + strconv.FormatInt(adminID, 10)
	expect(t, e.do(req{method: "POST", path: self + "/block", token: tok}), http.StatusForbidden, "forbidden")
	expect(t, e.do(req{method: "POST", path: "/v1/admin/users/999/block", token: tok}), http.StatusNotFound, "not_found")
	expect(t, e.do(req{method: "POST", path: "/v1/admin/users/" + strconv.FormatInt(userID, 10) + "/block", token: tok}),
		http.StatusNoContent, "")

	r = e.do(req{method: "GET", path: "/v1/admin/users", token: tok})
	items, _ = r.body["items"].([]any)
	if items[1].(map[string]any)["status"] != "blocked" {
		t.Fatalf("users after block: %s", r.raw)
	}
}

func TestRoutingAndLimits(t *testing.T) {
	e := newEnv(t)
	expect(t, e.do(req{method: "GET", path: "/v1/nope"}), http.StatusNotFound, "not_found")
	expect(t, e.do(req{method: "GET", path: "/v1/auth/register"}), http.StatusNotFound, "not_found")
	expect(t, e.do(req{method: "GET", path: "/healthz"}), http.StatusOK, "")
	expect(t, e.do(req{method: "GET", path: "/readyz"}), http.StatusOK, "")

	big := `{"invite_code":"` + strings.Repeat("A", maxBodyBytes) + `"}`
	expect(t, e.do(req{method: "POST", path: "/v1/auth/register", body: big}), http.StatusBadRequest, "invalid_request")
}

func (e *env) newAdmin() (string, int64) { return e.user(accounts.RoleAdmin) }

// noLink is a link server with no workers connected.
type noLink struct{}

func (noLink) ConnectedWorkers() []string                     { return nil }
func (noLink) BroadcastConfig(context.Context) error          { return nil }
func (noLink) Shutdown(context.Context, string, string) error { return nil }
