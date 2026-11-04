package updates_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	linkv1 "github.com/tuskar2321/quoker/gen/quoker/link/v1"
	"github.com/tuskar2321/quoker/internal/worker/telegram"
	"github.com/tuskar2321/quoker/internal/worker/updates"
)

// fakeBot serves getUpdates from a fixed backlog, honouring offset like
// Telegram: updates below offset are confirmed and never returned again.
type fakeBot struct {
	mu      sync.Mutex
	backlog []int64
	offsets []int64
	status  int
}

func (b *fakeBot) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var p struct {
		Offset int64 `json:"offset"`
	}
	_ = json.NewDecoder(r.Body).Decode(&p)
	b.mu.Lock()
	defer b.mu.Unlock()
	b.offsets = append(b.offsets, p.Offset)
	if b.status != 0 {
		w.WriteHeader(b.status)
		_, _ = fmt.Fprintf(w, `{"ok":false,"error_code":%d,"description":"Conflict: terminated by other getUpdates request"}`, b.status)
		return
	}
	var items []string
	for _, id := range b.backlog {
		if id >= p.Offset {
			items = append(items, fmt.Sprintf(`{"update_id":%d,"message":{"text":"hi"}}`, id))
		}
	}
	if len(items) == 0 {
		time.Sleep(20 * time.Millisecond) // a short "long poll"
	}
	_, _ = fmt.Fprintf(w, `{"ok":true,"result":[%s]}`, join(items))
}

func join(items []string) string {
	out := ""
	for i, s := range items {
		if i > 0 {
			out += ","
		}
		out += s
	}
	return out
}

// fakeCore is the link client: it records forwarded updates and acks them
// when autoAck is on.
type fakeCore struct {
	mu        sync.Mutex
	cfg       *linkv1.Config
	forwarded []int64
	autoAck   bool
	poller    *updates.Poller
}

func (c *fakeCore) CoreConfig() *linkv1.Config { c.mu.Lock(); defer c.mu.Unlock(); return c.cfg }
func (c *fakeCore) Connected() bool            { return true }
func (c *fakeCore) SendBotUpdate(id int64, _ []byte) error {
	c.mu.Lock()
	c.forwarded = append(c.forwarded, id)
	ack := c.autoAck
	c.mu.Unlock()
	if ack {
		go c.poller.Ack(id)
	}
	return nil
}

func run(t *testing.T, bot *fakeBot, core *fakeCore, d time.Duration) {
	t.Helper()
	srv := httptest.NewServer(bot)
	defer srv.Close()
	core.cfg = &linkv1.Config{PollUpdates: true, BotToken: "1:T", BotApiBase: srv.URL}
	core.poller = updates.New(telegram.New(srv.Client()), core, slog.New(slog.NewTextHandler(io.Discard, nil))).
		WithTimings(0, 10*time.Millisecond, 50*time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	core.poller.Run(ctx)
}

func TestOffsetAdvancesOnlyAfterAck(t *testing.T) {
	bot := &fakeBot{backlog: []int64{7, 8}}
	core := &fakeCore{autoAck: true}
	run(t, bot, core, 300*time.Millisecond)

	if len(core.forwarded) != 2 || core.forwarded[0] != 7 || core.forwarded[1] != 8 {
		t.Fatalf("forwarded = %v", core.forwarded)
	}
	if bot.offsets[0] != 0 || bot.offsets[1] != 9 {
		t.Fatalf("offsets = %v, want 0 then 9", bot.offsets)
	}
}

func TestNoAckRedelivers(t *testing.T) {
	bot := &fakeBot{backlog: []int64{7}}
	core := &fakeCore{}
	run(t, bot, core, 300*time.Millisecond)

	if len(core.forwarded) < 2 {
		t.Fatalf("update not re-sent without ack: %v", core.forwarded)
	}
	for _, o := range bot.offsets {
		if o != 0 {
			t.Fatalf("offset advanced without ack: %v", bot.offsets)
		}
	}
}

func TestIdleWhenNotPoller(t *testing.T) {
	bot := &fakeBot{backlog: []int64{1}}
	core := &fakeCore{}
	srv := httptest.NewServer(bot)
	defer srv.Close()
	core.cfg = &linkv1.Config{PollUpdates: false, BotToken: "1:T", BotApiBase: srv.URL}
	p := updates.New(telegram.New(srv.Client()), core, nil).WithTimings(0, 10*time.Millisecond, 10*time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	p.Run(ctx)
	if len(bot.offsets) != 0 {
		t.Fatalf("polled while not the poller: %v", bot.offsets)
	}
}

func TestConflictBacksOff(t *testing.T) {
	bot := &fakeBot{status: http.StatusConflict}
	core := &fakeCore{}
	run(t, bot, core, 100*time.Millisecond)
	if len(bot.offsets) == 0 || len(bot.offsets) > 15 {
		t.Fatalf("calls during conflict = %d", len(bot.offsets))
	}
}
