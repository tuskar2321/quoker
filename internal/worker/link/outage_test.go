package link_test

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	linkv1 "github.com/tuskar2321/quoker/gen/quoker/link/v1"
	workerlink "github.com/tuskar2321/quoker/internal/worker/link"
	"github.com/tuskar2321/quoker/internal/worker/telegram"
)

// flakyCore is a link server that can be switched off: while down it refuses
// streams, and drop ends the live one.
type flakyCore struct {
	linkv1.UnimplementedLinkServiceServer
	cfg  *linkv1.Config
	down atomic.Bool

	mu   sync.Mutex
	drop chan struct{}
}

func (f *flakyCore) Connect(stream linkv1.LinkService_ConnectServer) error {
	if f.down.Load() {
		return status.Error(codes.Unavailable, "core is down")
	}
	if _, err := stream.Recv(); err != nil { // Hello
		return err
	}
	drop := make(chan struct{})
	f.mu.Lock()
	f.drop = drop
	f.mu.Unlock()
	if err := stream.Send(&linkv1.CoreMessage{Msg: &linkv1.CoreMessage_Config{Config: f.cfg}}); err != nil {
		return err
	}
	select {
	case <-drop:
		return status.Error(codes.Unavailable, "dropped")
	case <-stream.Context().Done():
		return nil
	}
}

func (f *flakyCore) dropStream() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.drop != nil {
		close(f.drop)
		f.drop = nil
	}
}

// fakeTelegram records sendMessage calls.
type fakeTelegram struct {
	mu   sync.Mutex
	sent []sentMessage
	srv  *httptest.Server
}

type sentMessage struct {
	Path   string
	ChatID int64
	Text   string
}

func newFakeTelegram(t *testing.T) *fakeTelegram {
	t.Helper()
	ft := &fakeTelegram{}
	ft.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var p struct {
			ChatID int64  `json:"chat_id"`
			Text   string `json:"text"`
		}
		_ = json.Unmarshal(body, &p)
		ft.mu.Lock()
		ft.sent = append(ft.sent, sentMessage{Path: r.URL.Path, ChatID: p.ChatID, Text: p.Text})
		ft.mu.Unlock()
		_, _ = io.WriteString(w, `{"ok":true,"result":{"message_id":1}}`)
	}))
	t.Cleanup(ft.srv.Close)
	return ft
}

func (ft *fakeTelegram) messages() []sentMessage {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	return append([]sentMessage(nil), ft.sent...)
}

// TestCoreOutageAlert: the worker tells the admin once per outage that lasted
// past the threshold, using the Config it remembered (docs/02, REVIEW N2).
func TestCoreOutageAlert(t *testing.T) {
	const after = 2500 * time.Millisecond // reconnect after a drop takes about a second
	tests := []struct {
		name      string
		token     string
		chatID    int64
		neverUp   bool
		outages   []time.Duration // how long the core stays down, per outage
		wantSends int
	}{
		{name: "short outage", token: "123:TOKEN", chatID: 500, outages: []time.Duration{0}, wantSends: 0},
		{name: "long outage alerts once", token: "123:TOKEN", chatID: 500, outages: []time.Duration{3500 * time.Millisecond}, wantSends: 1},
		{name: "second outage alerts again", token: "123:TOKEN", chatID: 500,
			outages: []time.Duration{3500 * time.Millisecond, 3500 * time.Millisecond}, wantSends: 2},
		{name: "no admin chat", token: "123:TOKEN", chatID: 0, outages: []time.Duration{3500 * time.Millisecond}, wantSends: 0},
		{name: "no token", token: "", chatID: 500, outages: []time.Duration{3500 * time.Millisecond}, wantSends: 0},
		{name: "never connected", token: "123:TOKEN", chatID: 500, neverUp: true, wantSends: 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ft := newFakeTelegram(t)
			fc := &flakyCore{cfg: &linkv1.Config{BotToken: tc.token, AdminChatId: tc.chatID, BotApiBase: ft.srv.URL}}
			lis, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			gs := grpc.NewServer()
			linkv1.RegisterLinkServiceServer(gs, fc)
			go func() { _ = gs.Serve(lis) }()
			t.Cleanup(gs.Stop)

			client := workerlink.New(workerlink.Config{
				WorkerID: "worker-1", CoreAddr: lis.Addr().String(), Handler: &gatedHandler{},
				Telegram: telegram.New(&http.Client{}), OutageAfter: after,
			}, quiet, nil)
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			go func() { client.Run(ctx); close(done) }()
			defer func() { cancel(); <-done }()

			if tc.neverUp {
				fc.down.Store(true)
				time.Sleep(after + time.Second)
			} else {
				waitFor(t, "connect", client.Connected)
				for _, d := range tc.outages {
					fc.down.Store(true)
					fc.dropStream()
					waitFor(t, "disconnect", func() bool { return !client.Connected() })
					time.Sleep(d)
					fc.down.Store(false)
					waitFor(t, "reconnect", client.Connected)
				}
			}
			got := ft.messages()
			if len(got) != tc.wantSends {
				t.Fatalf("sent %d messages, want %d: %+v", len(got), tc.wantSends, got)
			}
			for _, m := range got {
				if m.ChatID != 500 || m.Text != "⚠️ Воркер worker-1 не может подключиться к ядру 5 минут" ||
					!strings.HasSuffix(m.Path, "/sendMessage") {
					t.Fatalf("unexpected message %+v", m)
				}
			}
		})
	}
}
