// Package botapitest plays a worker that answers bot_call tasks from the
// queue, so code that awaits Bot API results can be tested end to end.
package botapitest

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	linkv1 "github.com/tuskar2321/quoker/gen/quoker/link/v1"
	"github.com/tuskar2321/quoker/internal/core/queue"
)

// Reply is the fake worker's answer to one call.
type Reply struct {
	Result  any    // marshalled into response_json
	Content []byte // for file downloads
	ErrCode string // non-empty fails the task with this code
	ErrMsg  string // e.g. "telegram 400: Bad Request: chat not found"
}

// Call is a recorded bot_call.
type Call struct {
	Method   string
	Params   map[string]any
	Download string
}

// Worker answers bot_call tasks with Answer.
type Worker struct {
	Answer func(c Call) Reply

	mu    sync.Mutex
	calls []Call
}

// Calls returns the calls answered so far.
func (w *Worker) Calls() []Call {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]Call(nil), w.calls...)
}

// Methods returns the answered methods in order.
func (w *Worker) Methods() []string {
	var out []string
	for _, c := range w.Calls() {
		out = append(out, c.Method)
	}
	return out
}

// Start serves until the test ends.
func (w *Worker) Start(t *testing.T, q *queue.Store) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	t.Cleanup(func() { cancel(); <-done })
	go func() {
		defer close(done)
		for ctx.Err() == nil {
			task, err := q.Lease(ctx, "fake-worker")
			if errors.Is(err, queue.ErrNoTask) || err != nil {
				select {
				case <-ctx.Done():
				case <-time.After(5 * time.Millisecond):
				}
				continue
			}
			w.answer(ctx, q, task)
		}
	}()
}

func (w *Worker) answer(ctx context.Context, q *queue.Store, task *queue.Task) {
	p := queue.CompleteParams{TaskID: task.ID, WorkerID: "fake-worker"}
	if task.Kind != queue.KindBotCall {
		p.ErrCode, p.ErrMsg = "internal", "fake worker only answers bot_call"
		_ = q.Complete(ctx, p)
		return
	}
	var bc linkv1.BotCallTask
	_ = proto.Unmarshal(task.Payload, &bc)
	c := Call{Method: bc.Method, Download: bc.DownloadFilePath}
	_ = json.Unmarshal([]byte(bc.ParamsJson), &c.Params)
	w.mu.Lock()
	w.calls = append(w.calls, c)
	w.mu.Unlock()

	r := Reply{Result: true}
	if w.Answer != nil {
		r = w.Answer(c)
	}
	if r.ErrCode != "" {
		p.ErrCode, p.ErrMsg = r.ErrCode, r.ErrMsg
	} else {
		raw, _ := json.Marshal(r.Result)
		p.Result, _ = proto.Marshal(&linkv1.BotCallResult{ResponseJson: string(raw), FileContent: r.Content})
	}
	_ = q.Complete(ctx, p)
}
