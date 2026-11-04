package bot_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/tuskar2321/quoker/internal/core/bot"
	"github.com/tuskar2321/quoker/internal/core/settings"
	"github.com/tuskar2321/quoker/internal/core/store"
	"github.com/tuskar2321/quoker/internal/shared/errs"
)

func openTest(t *testing.T) (*store.Store, *settings.Store) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "q.db")
	st, err := store.Open(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	set := settings.New(st.DB(), nil)
	return st, set
}

// insertUser creates a minimal user row and returns its id.
func insertUser(t *testing.T, st *store.Store, now time.Time) int64 {
	t.Helper()
	res, err := st.DB().ExecContext(context.Background(),
		`INSERT INTO users (role, status, created_at) VALUES ('user','active',?)`,
		now.UnixMilli())
	if err != nil {
		t.Fatalf("insertUser: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

type fakeClock struct{ t time.Time }

func (c *fakeClock) Now() time.Time          { return c.t }
func (c *fakeClock) Advance(d time.Duration) { c.t = c.t.Add(d) }

func TestCreateLinkToken_noBotUsername(t *testing.T) {
	st, set := openTest(t)
	clk := &fakeClock{t: time.Now()}
	svc := bot.New(st, set, clk.Now)

	uid := insertUser(t, st, clk.Now())
	_, err := svc.CreateLinkToken(context.Background(), uid)
	if !errs.Is(err, errs.WorkerUnavailable) {
		t.Fatalf("expected worker_unavailable, got %v", err)
	}
}

func TestCreateLinkToken_ok(t *testing.T) {
	st, set := openTest(t)
	clk := &fakeClock{t: time.Now()}
	svc := bot.New(st, set, clk.Now)

	ctx := context.Background()
	if err := set.Set(ctx, settings.BotUsername, "quoker_bot"); err != nil {
		t.Fatal(err)
	}
	uid := insertUser(t, st, clk.Now())

	info, err := svc.CreateLinkToken(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	if info.DeepLink == "" {
		t.Fatal("empty deep link")
	}
	if !info.ExpiresAt.After(clk.Now()) {
		t.Fatal("expires_at not in the future")
	}
	// Should contain the bot username and "link_" prefix.
	if want := "https://t.me/quoker_bot?start=link_"; len(info.DeepLink) < len(want) {
		t.Fatalf("deep link too short: %q", info.DeepLink)
	}
}

func TestCreateLinkToken_invalidatesPreviousToken(t *testing.T) {
	st, set := openTest(t)
	clk := &fakeClock{t: time.Now()}
	svc := bot.New(st, set, clk.Now)

	ctx := context.Background()
	if err := set.Set(ctx, settings.BotUsername, "quoker_bot"); err != nil {
		t.Fatal(err)
	}
	uid := insertUser(t, st, clk.Now())

	_, _ = svc.CreateLinkToken(ctx, uid)
	_, _ = svc.CreateLinkToken(ctx, uid)

	// Only the most recent token should be unused.
	var count int
	if err := st.DB().QueryRowContext(ctx,
		`SELECT count(*) FROM link_tokens WHERE user_id = ? AND used_at IS NULL`, uid).
		Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected 1 unused token, got %d", count)
	}
}

func TestTelegramLinkStatus_pending(t *testing.T) {
	st, set := openTest(t)
	clk := &fakeClock{t: time.Now()}
	svc := bot.New(st, set, clk.Now)

	ctx := context.Background()
	if err := set.Set(ctx, settings.BotUsername, "quoker_bot"); err != nil {
		t.Fatal(err)
	}
	uid := insertUser(t, st, clk.Now())
	if _, err := svc.CreateLinkToken(ctx, uid); err != nil {
		t.Fatal(err)
	}

	info, err := svc.TelegramLinkStatus(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	if info.Status != bot.LinkPending {
		t.Fatalf("expected pending, got %q", info.Status)
	}
}

func TestTelegramLinkStatus_expired(t *testing.T) {
	st, set := openTest(t)
	clk := &fakeClock{t: time.Now()}
	svc := bot.New(st, set, clk.Now)

	ctx := context.Background()
	if err := set.Set(ctx, settings.BotUsername, "quoker_bot"); err != nil {
		t.Fatal(err)
	}
	uid := insertUser(t, st, clk.Now())
	if _, err := svc.CreateLinkToken(ctx, uid); err != nil {
		t.Fatal(err)
	}

	// Advance past TTL.
	clk.Advance(11 * time.Minute)

	info, err := svc.TelegramLinkStatus(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	if info.Status != bot.LinkExpired {
		t.Fatalf("expected expired, got %q", info.Status)
	}
}

func TestConsumeLinkToken_ok(t *testing.T) {
	st, set := openTest(t)
	clk := &fakeClock{t: time.Now()}
	svc := bot.New(st, set, clk.Now)

	ctx := context.Background()
	if err := set.Set(ctx, settings.BotUsername, "quoker_bot"); err != nil {
		t.Fatal(err)
	}
	uid := insertUser(t, st, clk.Now())

	info, err := svc.CreateLinkToken(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	// Extract the raw token from the deep link.
	const prefix = "https://t.me/quoker_bot?start=link_"
	token := info.DeepLink[len(prefix):]

	linkedUID, err := svc.ConsumeLinkToken(ctx, token, 42001, "maxim", "Max")
	if err != nil {
		t.Fatal(err)
	}
	if linkedUID != uid {
		t.Fatalf("linked uid = %d, want %d", linkedUID, uid)
	}

	// Status should now be linked.
	status, err := svc.TelegramLinkStatus(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	if status.Status != bot.LinkLinked {
		t.Fatalf("expected linked, got %q", status.Status)
	}
	if status.Username != "maxim" {
		t.Fatalf("username = %q", status.Username)
	}

	// A private target should have been created.
	var targetCount int
	if err = st.DB().QueryRowContext(ctx,
		`SELECT count(*) FROM targets WHERE owner_user_id = ? AND type = 'private'`, uid).
		Scan(&targetCount); err != nil {
		t.Fatal(err)
	}
	if targetCount != 1 {
		t.Fatalf("expected 1 private target, got %d", targetCount)
	}
}

func TestConsumeLinkToken_expiredToken(t *testing.T) {
	st, set := openTest(t)
	clk := &fakeClock{t: time.Now()}
	svc := bot.New(st, set, clk.Now)

	ctx := context.Background()
	if err := set.Set(ctx, settings.BotUsername, "quoker_bot"); err != nil {
		t.Fatal(err)
	}
	uid := insertUser(t, st, clk.Now())
	info, _ := svc.CreateLinkToken(ctx, uid)
	token := info.DeepLink[len("https://t.me/quoker_bot?start=link_"):]

	clk.Advance(11 * time.Minute)
	_, err := svc.ConsumeLinkToken(ctx, token, 999, "", "")
	if !errs.Is(err, errs.LinkTokenExpired) {
		t.Fatalf("expected link_token_expired, got %v", err)
	}
}

func TestConsumeLinkToken_alreadyBoundElsewhere(t *testing.T) {
	st, set := openTest(t)
	clk := &fakeClock{t: time.Now()}
	svc := bot.New(st, set, clk.Now)

	ctx := context.Background()
	if err := set.Set(ctx, settings.BotUsername, "quoker_bot"); err != nil {
		t.Fatal(err)
	}
	uid1 := insertUser(t, st, clk.Now())
	uid2 := insertUser(t, st, clk.Now())

	// Bind tg_user_id=55 to uid2 directly.
	_, err := st.DB().ExecContext(ctx,
		`UPDATE users SET tg_user_id = 55 WHERE id = ?`, uid2)
	if err != nil {
		t.Fatal(err)
	}

	// Now uid1 tries to link the same tg_user_id.
	info, _ := svc.CreateLinkToken(ctx, uid1)
	token := info.DeepLink[len("https://t.me/quoker_bot?start=link_"):]

	_, err = svc.ConsumeLinkToken(ctx, token, 55, "other", "Other")
	if !errs.Is(err, errs.Forbidden) {
		t.Fatalf("expected forbidden, got %v", err)
	}
}

func TestConsumeLinkToken_usedTwice(t *testing.T) {
	st, set := openTest(t)
	clk := &fakeClock{t: time.Now()}
	svc := bot.New(st, set, clk.Now)

	ctx := context.Background()
	if err := set.Set(ctx, settings.BotUsername, "quoker_bot"); err != nil {
		t.Fatal(err)
	}
	uid := insertUser(t, st, clk.Now())
	info, _ := svc.CreateLinkToken(ctx, uid)
	token := info.DeepLink[len("https://t.me/quoker_bot?start=link_"):]

	if _, err := svc.ConsumeLinkToken(ctx, token, 100, "u", "U"); err != nil {
		t.Fatal(err)
	}
	_, err := svc.ConsumeLinkToken(ctx, token, 100, "u", "U")
	if !errs.Is(err, errs.LinkTokenExpired) {
		t.Fatalf("expected link_token_expired on second use, got %v", err)
	}
}

func TestUnlinkTelegram(t *testing.T) {
	st, set := openTest(t)
	clk := &fakeClock{t: time.Now()}
	svc := bot.New(st, set, clk.Now)

	ctx := context.Background()
	if err := set.Set(ctx, settings.BotUsername, "quoker_bot"); err != nil {
		t.Fatal(err)
	}
	uid := insertUser(t, st, clk.Now())
	info, _ := svc.CreateLinkToken(ctx, uid)
	token := info.DeepLink[len("https://t.me/quoker_bot?start=link_"):]
	if _, err := svc.ConsumeLinkToken(ctx, token, 77, "u", "U"); err != nil {
		t.Fatal(err)
	}

	if err := svc.UnlinkTelegram(ctx, uid); err != nil {
		t.Fatal(err)
	}

	// User no longer linked.
	status, err := svc.TelegramLinkStatus(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	if status.Status == bot.LinkLinked {
		t.Fatal("expected unlinked, still shows linked")
	}

	// Targets deactivated.
	var activeTargets int
	if err = st.DB().QueryRowContext(ctx,
		`SELECT count(*) FROM targets WHERE owner_user_id = ? AND status = 'active'`, uid).
		Scan(&activeTargets); err != nil {
		t.Fatal(err)
	}
	if activeTargets != 0 {
		t.Fatalf("expected 0 active targets after unlink, got %d", activeTargets)
	}
}
