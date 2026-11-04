package accounts

import (
	"bytes"
	"context"
	"io"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/tuskar2321/quoker/internal/core/store"
	"github.com/tuskar2321/quoker/internal/shared/errs"
)

type fixture struct {
	t   *testing.T
	ctx context.Context
	st  *store.Store
	svc *Service
	now time.Time
}

func newFixture(t *testing.T, rnd io.Reader) *fixture {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "q.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	f := &fixture{t: t, ctx: context.Background(), st: st,
		now: time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)}
	f.svc = New(st, func() time.Time { return f.now }, rnd)
	return f
}

func (f *fixture) invite(role Role) string {
	f.t.Helper()
	code, _, err := f.svc.CreateInvite(f.ctx, 0, role, DefaultInviteTTL)
	if err != nil {
		f.t.Fatal(err)
	}
	return code
}

func (f *fixture) register(code string) (string, User) {
	f.t.Helper()
	tok, u, err := f.svc.Register(f.ctx, Registration{InviteCode: code, DeviceID: "dev", DeviceName: "Pixel"})
	if err != nil {
		f.t.Fatalf("Register(%s): %v", code, err)
	}
	return tok, u
}

func wantCode(t *testing.T, err error, code errs.Code) {
	t.Helper()
	if code == "" {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		return
	}
	if !errs.Is(err, code) {
		t.Fatalf("err = %v, want code %s", err, code)
	}
}

var codeRe = regexp.MustCompile(`^[A-HJ-NP-Z2-9]{4}-[A-HJ-NP-Z2-9]{4}$`)

func TestRegister(t *testing.T) {
	tests := []struct {
		name  string
		setup func(f *fixture) string // returns the code to redeem
		want  errs.Code
	}{
		{name: "valid", setup: func(f *fixture) string { return f.invite(RoleUser) }},
		{name: "lower case with spaces", setup: func(f *fixture) string {
			c := f.invite(RoleUser)
			return " " + string(bytes.ToLower([]byte(c[:4]))) + " " + c[5:]
		}},
		{name: "unknown", setup: func(*fixture) string { return "AAAA-BBBB" }, want: errs.InvalidInvite},
		{name: "malformed", setup: func(*fixture) string { return "AAAA-BBB!" }, want: errs.InvalidInvite},
		{name: "too short", setup: func(*fixture) string { return "AAAA-BBB" }, want: errs.InvalidInvite},
		{name: "expired", setup: func(f *fixture) string {
			c := f.invite(RoleUser)
			f.now = f.now.Add(DefaultInviteTTL)
			return c
		}, want: errs.InvalidInvite},
		{name: "used", setup: func(f *fixture) string {
			c := f.invite(RoleUser)
			f.register(c)
			return c
		}, want: errs.InvalidInvite},
		{name: "revoked", setup: func(f *fixture) string {
			c := f.invite(RoleUser)
			invs, _ := f.svc.ListInvites(f.ctx)
			if err := f.svc.RevokeInvite(f.ctx, invs[0].ID); err != nil {
				f.t.Fatal(err)
			}
			return c
		}, want: errs.InvalidInvite},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, nil)
			code := tt.setup(f)
			tok, u, err := f.svc.Register(f.ctx, Registration{InviteCode: code, DeviceID: "d", DeviceName: "n", AppVersion: 3})
			wantCode(t, err, tt.want)
			if tt.want != "" {
				return
			}
			if u.Role != RoleUser || u.Status != StatusActive || u.TelegramLinked() {
				t.Fatalf("user = %+v", u)
			}
			p, err := f.svc.Authenticate(f.ctx, tok, 3)
			if err != nil || p.UserID != u.ID {
				t.Fatalf("Authenticate = %+v, %v", p, err)
			}
		})
	}
}

func TestRegisterTakesInviteRole(t *testing.T) {
	f := newFixture(t, nil)
	_, u := f.register(f.invite(RoleAdmin))
	if u.Role != RoleAdmin {
		t.Fatalf("role = %s, want admin", u.Role)
	}
}

func TestAuthenticate(t *testing.T) {
	f := newFixture(t, nil)
	tok, u := f.register(f.invite(RoleUser))
	other, _ := f.register(f.invite(RoleUser))
	if err := f.svc.Logout(f.ctx, mustAuth(t, f, other).SessionID); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name  string
		token string
		want  errs.Code
	}{
		{"valid", tok, ""},
		{"empty", "", errs.Unauthorized},
		{"not base64", "!!!", errs.Unauthorized},
		{"unknown", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", errs.Unauthorized},
		{"logged out", other, errs.Unauthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := f.svc.Authenticate(f.ctx, tt.token, 0)
			wantCode(t, err, tt.want)
			if tt.want == "" && (p.UserID != u.ID || p.Role != RoleUser || p.IsAdmin()) {
				t.Fatalf("principal = %+v", p)
			}
		})
	}
}

func TestAuthenticateTouchesLastSeen(t *testing.T) {
	f := newFixture(t, nil)
	tok, u := f.register(f.invite(RoleUser))
	registered := f.now

	f.now = registered.Add(30 * time.Second) // throttled
	mustAuth(t, f, tok)
	if got := mustUser(t, f, u.ID).LastSeenAt; !got.Equal(registered) {
		t.Fatalf("last_seen = %v, want unchanged %v", got, registered)
	}
	f.now = registered.Add(2 * time.Minute)
	mustAuth(t, f, tok)
	if got := mustUser(t, f, u.ID).LastSeenAt; !got.Equal(f.now) {
		t.Fatalf("last_seen = %v, want %v", got, f.now)
	}
}

func TestBlockUnblock(t *testing.T) {
	f := newFixture(t, nil)
	_, admin := f.register(f.invite(RoleAdmin))
	code := f.invite(RoleUser)
	phone, u := f.register(code)
	// A second device of the same user: sessions are per device.
	tablet, err := f.addSession(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Logout(f.ctx, mustAuth(t, f, tablet).SessionID); err != nil {
		t.Fatal(err)
	}

	wantCode(t, f.svc.Block(f.ctx, admin.ID, admin.ID), errs.Forbidden)
	wantCode(t, f.svc.Block(f.ctx, admin.ID, 999), errs.NotFound)
	wantCode(t, f.svc.Unblock(f.ctx, 999), errs.NotFound)

	wantCode(t, f.svc.Block(f.ctx, admin.ID, u.ID), "")
	wantCode(t, f.svc.Block(f.ctx, admin.ID, u.ID), "") // idempotent
	if got := mustUser(t, f, u.ID).Status; got != StatusBlocked {
		t.Fatalf("status = %s", got)
	}
	for _, tok := range []string{phone, tablet} {
		_, err := f.svc.Authenticate(f.ctx, tok, 0)
		wantCode(t, err, errs.UserBlocked)
	}

	wantCode(t, f.svc.Unblock(f.ctx, u.ID), "")
	if _, err := f.svc.Authenticate(f.ctx, phone, 0); err != nil {
		t.Fatalf("session revoked by the block must be restored: %v", err)
	}
	_, err = f.svc.Authenticate(f.ctx, tablet, 0)
	wantCode(t, err, errs.Unauthorized) // logout is permanent
}

func TestInvites(t *testing.T) {
	f := newFixture(t, nil)
	code, expires, err := f.svc.CreateInvite(f.ctx, 0, RoleUser, 3*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if !codeRe.MatchString(code) {
		t.Fatalf("code %q does not match %s", code, codeRe)
	}
	if !expires.Equal(f.now.Add(3 * 24 * time.Hour)) {
		t.Fatalf("expires = %v", expires)
	}
	_, _, err = f.svc.CreateInvite(f.ctx, 0, Role("root"), time.Hour)
	wantCode(t, err, errs.InvalidRequest)

	usedCode := f.invite(RoleUser)
	_, user := f.register(usedCode)

	invs, err := f.svc.ListInvites(f.ctx)
	if err != nil || len(invs) != 2 {
		t.Fatalf("ListInvites = %d items, %v", len(invs), err)
	}
	byPrefix := map[string]Invite{}
	for _, inv := range invs {
		byPrefix[inv.CodePrefix] = inv
	}
	fresh, used := byPrefix[code[:4]], byPrefix[usedCode[:4]]
	if fresh.UsedBy != 0 || !fresh.UsedAt.IsZero() {
		t.Fatalf("fresh invite = %+v", fresh)
	}
	if used.UsedBy != user.ID || used.UsedAt.IsZero() {
		t.Fatalf("used invite = %+v", used)
	}

	wantCode(t, f.svc.RevokeInvite(f.ctx, used.ID), "") // used: no-op
	wantCode(t, f.svc.RevokeInvite(f.ctx, fresh.ID), "")
	wantCode(t, f.svc.RevokeInvite(f.ctx, 999), errs.NotFound)
	invs, _ = f.svc.ListInvites(f.ctx)
	if len(invs) != 1 || invs[0].ID != used.ID {
		t.Fatalf("after revoke: %+v", invs)
	}
}

func TestCreateInviteRetriesOnCollision(t *testing.T) {
	same := bytes.Repeat([]byte{1}, inviteLen)
	other := bytes.Repeat([]byte{2}, inviteLen)
	rnd := io.MultiReader(bytes.NewReader(same), bytes.NewReader(same), bytes.NewReader(other))
	f := newFixture(t, rnd)
	a := f.invite(RoleUser)
	b := f.invite(RoleUser)
	if a == b || a != "BBBB-BBBB" || b != "CCCC-CCCC" {
		t.Fatalf("codes = %s, %s", a, b)
	}
}

func TestCreateBootstrapInvite(t *testing.T) {
	f := newFixture(t, nil)
	code, _, err := f.svc.CreateBootstrapInvite(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, u := f.register(code)
	if u.Role != RoleAdmin {
		t.Fatalf("bootstrap user role = %s", u.Role)
	}
	_, _, err = f.svc.CreateBootstrapInvite(f.ctx)
	wantCode(t, err, errs.Forbidden)
}

func TestNormalizeInviteCode(t *testing.T) {
	tests := []struct {
		in     string
		want   string
		wantOK bool
	}{
		{"K7Q2-M4XP", "K7Q2M4XP", true},
		{"k7q2m4xp", "K7Q2M4XP", true},
		{" k7q2 - m4xp ", "K7Q2M4XP", true},
		{"K7Q2-M4X", "", false},
		{"K7Q2-M4XPP", "", false},
		{"K7Q2_M4XP", "", false},
		{"К7Q2-M4XP", "", false}, // Cyrillic К
	}
	for _, tt := range tests {
		got, ok := normalizeInviteCode(tt.in)
		if got != tt.want || ok != tt.wantOK {
			t.Errorf("normalizeInviteCode(%q) = %q, %v; want %q, %v", tt.in, got, ok, tt.want, tt.wantOK)
		}
	}
}

// addSession simulates the same user signing in on another device.
func (f *fixture) addSession(userID int64) (string, error) {
	tok, hash, err := f.svc.newToken()
	if err != nil {
		return "", err
	}
	_, err = f.st.DB().ExecContext(f.ctx,
		`INSERT INTO sessions (user_id, token_hash, device_id, device_name, created_at, last_seen_at)
		 VALUES (?, ?, 'tablet', 'Tab', ?, ?)`, userID, hash, f.now.UnixMilli(), f.now.UnixMilli())
	return tok, err
}

func mustAuth(t *testing.T, f *fixture, tok string) Principal {
	t.Helper()
	p, err := f.svc.Authenticate(f.ctx, tok, 0)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	return p
}

func mustUser(t *testing.T, f *fixture, id int64) User {
	t.Helper()
	u, err := f.svc.User(f.ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return u
}
