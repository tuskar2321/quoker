package targets_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/tuskar2321/quoker/internal/core/store"
	"github.com/tuskar2321/quoker/internal/core/targets"
	"github.com/tuskar2321/quoker/internal/shared/errs"
)

type fakeClock struct{ t time.Time }

func (c *fakeClock) Now() time.Time          { return c.t }
func (c *fakeClock) Advance(d time.Duration) { c.t = c.t.Add(d) }

func openTest(t *testing.T) (*store.Store, *targets.Service, *fakeClock) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "q.db")
	st, err := store.Open(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	clk := &fakeClock{t: time.Now()}
	svc := targets.New(st, clk.Now)
	return st, svc, clk
}

// insertUser inserts a minimal user row and returns its id.
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

func TestList_empty(t *testing.T) {
	st, svc, clk := openTest(t)
	uid := insertUser(t, st, clk.Now())
	items, err := svc.List(context.Background(), uid, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("expected empty list, got %d items", len(items))
	}
}

func TestUpsert_insertAndGet(t *testing.T) {
	st, svc, clk := openTest(t)
	uid := insertUser(t, st, clk.Now())
	ctx := context.Background()

	got, err := svc.Upsert(ctx, targets.Target{
		OwnerUserID: uid,
		ChatID:      -1001,
		Type:        "group",
		Title:       "My Group",
		Status:      targets.StatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.ID == 0 {
		t.Fatal("expected non-zero ID")
	}
	if got.Title != "My Group" || got.Type != "group" {
		t.Fatalf("unexpected fields: %+v", got)
	}
	if got.Status != targets.StatusActive {
		t.Fatalf("status = %q", got.Status)
	}
}

func TestUpsert_idempotent(t *testing.T) {
	st, svc, clk := openTest(t)
	uid := insertUser(t, st, clk.Now())
	ctx := context.Background()

	first, _ := svc.Upsert(ctx, targets.Target{
		OwnerUserID: uid, ChatID: -1002, Type: "group", Title: "A", Status: targets.StatusActive,
	})
	second, err := svc.Upsert(ctx, targets.Target{
		OwnerUserID: uid, ChatID: -1002, Type: "channel", Title: "B", Status: targets.StatusInactive,
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("expected same ID on upsert, got %d vs %d", first.ID, second.ID)
	}
	// Fields should be updated.
	if second.Title != "B" || second.Type != "channel" || second.Status != targets.StatusInactive {
		t.Fatalf("upsert did not update: %+v", second)
	}
}

func TestList_excludesHidden(t *testing.T) {
	st, svc, clk := openTest(t)
	uid := insertUser(t, st, clk.Now())
	ctx := context.Background()

	visible, _ := svc.Upsert(ctx, targets.Target{OwnerUserID: uid, ChatID: -100, Type: "group", Title: "Visible", Status: targets.StatusActive})
	hidden, _ := svc.Upsert(ctx, targets.Target{OwnerUserID: uid, ChatID: -101, Type: "group", Title: "Hidden", Status: targets.StatusActive})

	// Hide the second.
	if _, err := svc.SetHidden(ctx, uid, hidden.ID, true); err != nil {
		t.Fatal(err)
	}

	// Without includeHidden.
	items, err := svc.List(ctx, uid, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != visible.ID {
		t.Fatalf("expected only visible, got %v", items)
	}

	// With includeHidden.
	all, err := svc.List(ctx, uid, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 with includeHidden, got %d", len(all))
	}
}

func TestList_sortOrder(t *testing.T) {
	st, svc, clk := openTest(t)
	uid := insertUser(t, st, clk.Now())
	ctx := context.Background()

	a, _ := svc.Upsert(ctx, targets.Target{OwnerUserID: uid, ChatID: -200, Type: "group", Title: "Alpha", Status: targets.StatusActive})
	b, _ := svc.Upsert(ctx, targets.Target{OwnerUserID: uid, ChatID: -201, Type: "group", Title: "Beta", Status: targets.StatusActive})

	// Simulate last_used_at: set it on Beta only.
	if _, err := st.DB().ExecContext(ctx,
		`UPDATE targets SET last_used_at = ? WHERE id = ?`,
		clk.Now().UnixMilli(), b.ID); err != nil {
		t.Fatal(err)
	}

	items, err := svc.List(ctx, uid, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatal("expected 2 items")
	}
	// Beta (recently used) should come first.
	if items[0].ID != b.ID || items[1].ID != a.ID {
		t.Fatalf("wrong order: got [%d,%d], want [%d,%d]", items[0].ID, items[1].ID, b.ID, a.ID)
	}
}

func TestSetHidden_notFound(t *testing.T) {
	st, svc, clk := openTest(t)
	uid := insertUser(t, st, clk.Now())
	_, err := svc.SetHidden(context.Background(), uid, 9999, true)
	if !errs.Is(err, errs.NotFound) {
		t.Fatalf("expected not_found, got %v", err)
	}
}

func TestSetHidden_ownershipEnforced(t *testing.T) {
	st, svc, clk := openTest(t)
	uid1 := insertUser(t, st, clk.Now())
	uid2 := insertUser(t, st, clk.Now())
	ctx := context.Background()

	tgt, _ := svc.Upsert(ctx, targets.Target{OwnerUserID: uid1, ChatID: -300, Type: "group", Title: "G", Status: targets.StatusActive})

	_, err := svc.SetHidden(ctx, uid2, tgt.ID, true)
	if !errs.Is(err, errs.NotFound) {
		t.Fatalf("expected not_found for wrong owner, got %v", err)
	}
}

func TestSetStatus(t *testing.T) {
	st, svc, clk := openTest(t)
	uid := insertUser(t, st, clk.Now())
	ctx := context.Background()

	tgt, _ := svc.Upsert(ctx, targets.Target{OwnerUserID: uid, ChatID: -400, Type: "group", Title: "G", Status: targets.StatusActive})

	if err := svc.SetStatus(ctx, tgt.ChatID, targets.StatusInactive, targets.ReasonBotRemoved); err != nil {
		t.Fatal(err)
	}

	got, err := svc.Get(ctx, uid, tgt.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != targets.StatusInactive {
		t.Fatalf("expected inactive, got %q", got.Status)
	}
	if got.InactiveReason != targets.ReasonBotRemoved {
		t.Fatalf("expected bot_removed, got %q", got.InactiveReason)
	}
}

func TestUpdateChatID(t *testing.T) {
	st, svc, clk := openTest(t)
	uid := insertUser(t, st, clk.Now())
	ctx := context.Background()

	tgt, _ := svc.Upsert(ctx, targets.Target{OwnerUserID: uid, ChatID: -500, Type: "group", Title: "G", Status: targets.StatusActive})

	if err := svc.UpdateChatID(ctx, -500, -99999); err != nil {
		t.Fatal(err)
	}

	got, err := svc.Get(ctx, uid, tgt.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ChatID != -99999 {
		t.Fatalf("chat_id = %d, want -99999", got.ChatID)
	}
}

func TestUpdateTitle(t *testing.T) {
	st, svc, clk := openTest(t)
	uid := insertUser(t, st, clk.Now())
	ctx := context.Background()

	tgt, _ := svc.Upsert(ctx, targets.Target{OwnerUserID: uid, ChatID: -600, Type: "group", Title: "Old", Status: targets.StatusActive})

	if err := svc.UpdateTitle(ctx, tgt.ChatID, "New Title"); err != nil {
		t.Fatal(err)
	}

	got, err := svc.Get(ctx, uid, tgt.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "New Title" {
		t.Fatalf("title = %q, want New Title", got.Title)
	}
}

func TestList_isolatedByUser(t *testing.T) {
	st, svc, clk := openTest(t)
	uid1 := insertUser(t, st, clk.Now())
	uid2 := insertUser(t, st, clk.Now())
	ctx := context.Background()

	if _, err := svc.Upsert(ctx, targets.Target{OwnerUserID: uid1, ChatID: -700, Type: "group", Title: "G1", Status: targets.StatusActive}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Upsert(ctx, targets.Target{OwnerUserID: uid2, ChatID: -700, Type: "group", Title: "G1", Status: targets.StatusActive}); err != nil {
		t.Fatal(err)
	}

	items1, _ := svc.List(ctx, uid1, false)
	items2, _ := svc.List(ctx, uid2, false)
	if len(items1) != 1 || len(items2) != 1 {
		t.Fatalf("isolation failed: uid1=%d uid2=%d", len(items1), len(items2))
	}
	if items1[0].ID == items2[0].ID {
		t.Fatal("expected different target rows for different users")
	}
}

// TestUpdateChatID_alreadyConnected: one owner already has the new
// supergroup, another does not; neither breaks the migration (REVIEW M15).
func TestUpdateChatID_alreadyConnected(t *testing.T) {
	st, svc, clk := openTest(t)
	ctx := context.Background()
	early := insertUser(t, st, clk.Now())
	late := insertUser(t, st, clk.Now())
	oldEarly, _ := svc.Upsert(ctx, targets.Target{OwnerUserID: early, ChatID: -500, Type: "group", Title: "G", Status: targets.StatusActive})
	newEarly, _ := svc.Upsert(ctx, targets.Target{OwnerUserID: early, ChatID: -100500, Type: "group", Title: "G", Status: targets.StatusActive})
	oldLate, _ := svc.Upsert(ctx, targets.Target{OwnerUserID: late, ChatID: -500, Type: "group", Title: "G", Status: targets.StatusActive})

	if err := svc.UpdateChatID(ctx, -500, -100500); err != nil {
		t.Fatalf("migration failed: %v", err)
	}
	if got, _ := svc.Get(ctx, early, oldEarly.ID); got.Status != targets.StatusInactive {
		t.Fatalf("duplicate old target = %+v", got)
	}
	if got, _ := svc.Get(ctx, early, newEarly.ID); got.Status != targets.StatusActive {
		t.Fatalf("new target = %+v", got)
	}
	if got, _ := svc.Get(ctx, late, oldLate.ID); got.ChatID != -100500 || got.Status != targets.StatusActive {
		t.Fatalf("moved target = %+v", got)
	}
}
