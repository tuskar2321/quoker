package targets_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/tuskar2321/quoker/internal/core/targets"
	"github.com/tuskar2321/quoker/internal/shared/errs"
)

// fakeBot answers getChatMember from members[chatID].
type fakeBot struct {
	members map[int64]string // chat → JSON of the member, or "!<description>" for an error
	calls   int
}

type botErr struct{ desc string }

func (e *botErr) Error() string          { return e.desc }
func (e *botErr) Mentions(s string) bool { return strings.Contains(strings.ToLower(e.desc), s) }
func (b *fakeBot) Call(_ context.Context, method string, params any) (json.RawMessage, error) {
	b.calls++
	if method != "getChatMember" {
		return nil, &botErr{"unexpected " + method}
	}
	chatID := params.(map[string]int64)["chat_id"]
	m, ok := b.members[chatID]
	switch {
	case !ok:
		return nil, errs.New(errs.WorkerUnavailable, "no worker")
	case strings.HasPrefix(m, "!"):
		return nil, &botErr{m[1:]}
	}
	return json.RawMessage(m), nil
}

func TestCheckRights(t *testing.T) {
	tests := []struct {
		name       string
		typ        string
		member     string
		wantErr    errs.Code
		wantStatus targets.Status
		wantReason targets.InactiveReason
	}{
		{"group member", "group", `{"status":"member"}`, "", targets.StatusActive, ""},
		{"group restricted with media", "group", `{"status":"restricted","can_send_videos":true}`, "", targets.StatusActive, ""},
		{"group restricted without media", "group", `{"status":"restricted","can_send_videos":false}`, errs.TargetForbidden, targets.StatusInactive, targets.ReasonUserLeft},
		{"group left", "group", `{"status":"left"}`, errs.TargetForbidden, targets.StatusInactive, targets.ReasonUserLeft},
		{"user not found", "group", `!telegram 400: Bad Request: user not found`, errs.TargetForbidden, targets.StatusInactive, targets.ReasonUserLeft},
		{"channel admin who posts", "channel", `{"status":"administrator","can_post_messages":true}`, "", targets.StatusActive, ""},
		{"channel admin without posting", "channel", `{"status":"administrator"}`, errs.TargetForbidden, targets.StatusInactive, targets.ReasonUserNotAdmin},
		{"channel member", "channel", `{"status":"member"}`, errs.TargetForbidden, targets.StatusInactive, targets.ReasonUserNotAdmin},
		{"bot kicked", "group", `!telegram 403: Forbidden: bot was kicked from the group chat`, errs.TargetInactive, targets.StatusInactive, targets.ReasonBotRemoved},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st, svc, clk := openTest(t)
			bot := &fakeBot{members: map[int64]string{-1: tt.member}}
			svc.WithBot(bot)
			uid := insertUser(t, st, clk.Now())
			ctx := context.Background()
			tgt, _ := svc.Upsert(ctx, targets.Target{OwnerUserID: uid, ChatID: -1, Type: tt.typ, Title: "C", Status: targets.StatusActive})

			err := svc.CheckRights(ctx, tgt, 42)
			if (tt.wantErr == "") != (err == nil) || (tt.wantErr != "" && !errs.Is(err, tt.wantErr)) {
				t.Fatalf("err = %v, want %q", err, tt.wantErr)
			}
			got, _ := svc.Get(ctx, uid, tgt.ID)
			if got.Status != tt.wantStatus || got.InactiveReason != tt.wantReason {
				t.Fatalf("target = %s/%s, want %s/%s", got.Status, got.InactiveReason, tt.wantStatus, tt.wantReason)
			}
		})
	}
}

func TestCheckRights_cacheAndSkips(t *testing.T) {
	st, svc, clk := openTest(t)
	bot := &fakeBot{members: map[int64]string{-1: `{"status":"member"}`}}
	svc.WithBot(bot)
	uid := insertUser(t, st, clk.Now())
	ctx := context.Background()
	tgt, _ := svc.Upsert(ctx, targets.Target{OwnerUserID: uid, ChatID: -1, Type: "group", Title: "G", Status: targets.StatusActive})

	if err := svc.CheckRights(ctx, tgt, 42); err != nil {
		t.Fatal(err)
	}
	tgt, _ = svc.Get(ctx, uid, tgt.ID)
	clk.Advance(4 * time.Minute)
	_ = svc.CheckRights(ctx, tgt, 42)
	if bot.calls != 1 {
		t.Fatalf("calls within 5 min = %d, want 1", bot.calls)
	}
	clk.Advance(2 * time.Minute)
	_ = svc.CheckRights(ctx, tgt, 42)
	if bot.calls != 2 {
		t.Fatalf("calls after 5 min = %d, want 2", bot.calls)
	}

	// No worker: the send goes ahead.
	other, _ := svc.Upsert(ctx, targets.Target{OwnerUserID: uid, ChatID: -2, Type: "group", Title: "H", Status: targets.StatusActive})
	if err := svc.CheckRights(ctx, other, 42); err != nil {
		t.Fatalf("no worker: err = %v", err)
	}
	// The bot's own chat needs no check.
	private, _ := svc.Upsert(ctx, targets.Target{OwnerUserID: uid, ChatID: 42, Type: "private", Title: "P", Status: targets.StatusActive})
	before := bot.calls
	if err := svc.CheckRights(ctx, private, 42); err != nil || bot.calls != before {
		t.Fatalf("private: err = %v, calls %d → %d", err, before, bot.calls)
	}
}

func TestRefresh(t *testing.T) {
	st, svc, clk := openTest(t)
	ctx := context.Background()
	me := insertUser(t, st, clk.Now())
	someone := insertUser(t, st, clk.Now())

	back, _ := svc.Upsert(ctx, targets.Target{OwnerUserID: me, ChatID: -1, Type: "group", Title: "Back", Status: targets.StatusActive})
	_ = svc.SetStatus(ctx, -1, targets.StatusInactive, targets.ReasonUserLeft)
	gone, _ := svc.Upsert(ctx, targets.Target{OwnerUserID: me, ChatID: -2, Type: "group", Title: "Gone", Status: targets.StatusActive})
	// A channel someone else connected, where I am an admin too.
	_, _ = svc.Upsert(ctx, targets.Target{OwnerUserID: someone, ChatID: -3, Type: "channel", Title: "News", Status: targets.StatusActive})

	svc.WithBot(&fakeBot{members: map[int64]string{
		-1: `{"status":"member"}`,
		-2: `{"status":"left"}`,
		-3: `{"status":"administrator","can_post_messages":true}`,
	}})
	if err := svc.Refresh(ctx, me, 42); err != nil {
		t.Fatal(err)
	}
	if got, _ := svc.Get(ctx, me, back.ID); got.Status != targets.StatusActive || got.InactiveReason != "" {
		t.Fatalf("regained target = %+v", got)
	}
	if got, _ := svc.Get(ctx, me, gone.ID); got.Status != targets.StatusInactive || got.InactiveReason != targets.ReasonUserLeft {
		t.Fatalf("lost target = %+v", got)
	}
	items, _ := svc.List(ctx, me, false)
	found := false
	for _, it := range items {
		found = found || (it.ChatID == -3 && it.Type == "channel" && it.Title == "News")
	}
	if !found {
		t.Fatalf("channel not discovered: %+v", items)
	}
}

func TestReactivateBotAndUpsertClearsReason(t *testing.T) {
	st, svc, clk := openTest(t)
	ctx := context.Background()
	uid := insertUser(t, st, clk.Now())
	tgt, _ := svc.Upsert(ctx, targets.Target{OwnerUserID: uid, ChatID: -1, Type: "group", Title: "G", Status: targets.StatusActive})

	_ = svc.SetStatus(ctx, -1, targets.StatusInactive, targets.ReasonBotRemoved)
	if err := svc.ReactivateBot(ctx, -1); err != nil {
		t.Fatal(err)
	}
	if got, _ := svc.Get(ctx, uid, tgt.ID); got.Status != targets.StatusActive || got.InactiveReason != "" {
		t.Fatalf("after ReactivateBot: %+v", got)
	}

	_ = svc.SetStatus(ctx, -1, targets.StatusInactive, targets.ReasonUserLeft)
	_ = svc.ReactivateBot(ctx, -1) // a user-side reason is not the bot's to clear
	if got, _ := svc.Get(ctx, uid, tgt.ID); got.Status != targets.StatusInactive {
		t.Fatalf("ReactivateBot revived a user_left target: %+v", got)
	}
	got, _ := svc.Upsert(ctx, targets.Target{OwnerUserID: uid, ChatID: -1, Type: "group", Title: "G", Status: targets.StatusActive})
	if got.InactiveReason != "" {
		t.Fatalf("Upsert kept reason %q", got.InactiveReason)
	}
}
