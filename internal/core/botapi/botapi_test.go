package botapi_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/tuskar2321/quoker/internal/core/botapi"
	"github.com/tuskar2321/quoker/internal/core/botapi/botapitest"
	"github.com/tuskar2321/quoker/internal/core/queue"
	"github.com/tuskar2321/quoker/internal/core/store"
	"github.com/tuskar2321/quoker/internal/shared/errs"
)

func newQueue(t *testing.T) *queue.Store {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "q.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return queue.New(st, nil)
}

func TestCall(t *testing.T) {
	q := newQueue(t)
	w := &botapitest.Worker{Answer: func(c botapitest.Call) botapitest.Reply {
		switch c.Method {
		case "getMe":
			return botapitest.Reply{Result: map[string]string{"username": "quoker_bot"}}
		case "getFile":
			if c.Download != "" {
				return botapitest.Reply{Content: []byte("file body")}
			}
			return botapitest.Reply{Result: map[string]string{"file_path": "documents/file_1.txt"}}
		}
		return botapitest.Reply{ErrCode: "upstream_error", ErrMsg: "telegram 400: Bad Request: chat not found"}
	}}
	w.Start(t, q)
	c := botapi.New(q)
	ctx := context.Background()

	raw, err := c.Call(ctx, "getMe", struct{}{})
	if err != nil || string(raw) != `{"username":"quoker_bot"}` {
		t.Fatalf("getMe = %s, %v", raw, err)
	}
	b, err := c.DownloadFile(ctx, "FILE-ID")
	if err != nil || string(b) != "file body" {
		t.Fatalf("download = %q, %v", b, err)
	}
	calls := w.Calls()
	if calls[1].Params["file_id"] != "FILE-ID" || calls[2].Download != "documents/file_1.txt" {
		t.Fatalf("calls = %+v", calls)
	}

	_, err = c.Call(ctx, "getChat", map[string]int{"chat_id": 1})
	var be *botapi.Error
	if !errors.As(err, &be) || !be.Mentions("CHAT NOT FOUND") || be.Code != errs.UpstreamError {
		t.Fatalf("err = %v", err)
	}
}

func TestCall_noWorker(t *testing.T) {
	c := botapi.New(newQueue(t))
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if _, err := c.Call(ctx, "getMe", struct{}{}); !errs.Is(err, errs.WorkerUnavailable) {
		t.Fatalf("err = %v, want worker_unavailable", err)
	}
}

func TestSendAndDedup(t *testing.T) {
	q := newQueue(t)
	c := botapi.New(q)
	ctx := context.Background()
	id1, err := c.Enqueue(ctx, "setMyCommands", map[string]any{"commands": []string{}}, "commands:private")
	if err != nil {
		t.Fatal(err)
	}
	id2, _ := c.Enqueue(ctx, "setMyCommands", map[string]any{"commands": []string{}}, "commands:private")
	if id1 != id2 {
		t.Fatalf("dedup failed: %s vs %s", id1, id2)
	}
	if err := c.Send(ctx, botapi.Message{ChatID: 5, Text: "hi", ReplyTo: 9}); err != nil {
		t.Fatal(err)
	}
	w := &botapitest.Worker{}
	w.Start(t, q)
	deadline := time.Now().Add(2 * time.Second)
	for len(w.Calls()) < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	send := w.Calls()[1]
	reply, _ := send.Params["reply_parameters"].(map[string]any)
	if send.Method != "sendMessage" || send.Params["text"] != "hi" || reply["message_id"] != float64(9) {
		t.Fatalf("sendMessage = %+v", send)
	}
}
