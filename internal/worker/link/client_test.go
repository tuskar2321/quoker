package link_test

import (
	"context"
	"io"
	"log/slog"
	"net"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"

	linkv1 "github.com/tuskar2321/quoker/gen/quoker/link/v1"
	corelink "github.com/tuskar2321/quoker/internal/core/link"
	"github.com/tuskar2321/quoker/internal/core/queue"
	"github.com/tuskar2321/quoker/internal/core/settings"
	"github.com/tuskar2321/quoker/internal/core/store"
	workerlink "github.com/tuskar2321/quoker/internal/worker/link"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

// core is a real link server on a local port.
type core struct {
	srv  *corelink.Server
	q    *queue.Store
	addr string
}

func startCore(t *testing.T) *core {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "q.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	// In dev mode the worker sends its id as the fingerprint.
	if _, err := st.DB().ExecContext(context.Background(),
		`INSERT INTO workers (id, fingerprint, status, created_at) VALUES ('w1', 'w1', 'allowed', 0)`); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	q := queue.New(st, nil)
	srv := corelink.New(ctx, corelink.Deps{Store: st, Queue: q, Settings: settings.New(st.DB(), nil), Log: quiet})
	lis, err := (&net.ListenConfig{}).Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	gs := grpc.NewServer()
	linkv1.RegisterLinkServiceServer(gs, srv)
	go func() { _ = gs.Serve(lis) }()
	t.Cleanup(gs.Stop)
	return &core{srv: srv, q: q, addr: lis.Addr().String()}
}

// gatedHandler finishes a task only when release is closed.
type gatedHandler struct {
	started chan struct{}
	release chan struct{}
}

func (h *gatedHandler) Handle(ctx context.Context, task *linkv1.Task, conn workerlink.Conn) error {
	close(h.started)
	select {
	case <-h.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	return conn.SendResult(&linkv1.TaskResult{TaskId: task.TaskId,
		Result: &linkv1.TaskResult_SendVideo{SendVideo: &linkv1.SendVideoResult{MessageId: 7}}})
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestResultSurvivesReconnect: a send finished while the stream was down is
// delivered after reconnecting, so the core never reruns it (REVIEW H5).
func TestResultSurvivesReconnect(t *testing.T) {
	c := startCore(t)
	payload, _ := proto.Marshal(&linkv1.SendVideoTask{VideoId: "tiktok:1"})
	id, err := c.q.Enqueue(context.Background(), queue.EnqueueParams{Kind: queue.KindSendVideo, Payload: payload})
	if err != nil {
		t.Fatal(err)
	}
	h := &gatedHandler{started: make(chan struct{}), release: make(chan struct{})}
	client := workerlink.New(workerlink.Config{WorkerID: "w1", CoreAddr: c.addr, Handler: h}, quiet, nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { client.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()

	<-h.started
	_ = c.srv.Shutdown(context.Background(), "w1", "restart") // the stream goes away mid-task
	waitFor(t, "disconnect", func() bool { return !client.Connected() })
	close(h.release) // the video is sent while disconnected

	waitFor(t, "task done", func() bool {
		task, err := c.q.Get(context.Background(), id)
		return err == nil && task.Status == queue.StatusDone
	})
}

// TestRevokedWorkerStops: after a "revoked" Shutdown the client exits
// instead of reconnecting forever (REVIEW M18).
func TestRevokedWorkerStops(t *testing.T) {
	c := startCore(t)
	var connects atomic.Int32
	client := workerlink.New(workerlink.Config{WorkerID: "w1", CoreAddr: c.addr, Handler: &gatedHandler{}}, quiet, nil)
	done := make(chan struct{})
	go func() { client.Run(context.Background()); close(done) }()
	waitFor(t, "connect", func() bool {
		if client.Connected() {
			connects.Add(1)
			return true
		}
		return false
	})
	_ = c.srv.Shutdown(context.Background(), "w1", "revoked")
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("revoked client kept running")
	}
}

// sleepyHandler finishes a send after delay unless cancelled first.
type sleepyHandler struct {
	delay   time.Duration
	started chan struct{}
}

func (h *sleepyHandler) Handle(ctx context.Context, task *linkv1.Task, conn workerlink.Conn) error {
	close(h.started)
	select {
	case <-time.After(h.delay):
	case <-ctx.Done():
		return conn.SendResult(&linkv1.TaskResult{TaskId: task.TaskId,
			Result: &linkv1.TaskResult_Error{Error: &linkv1.TaskError{Code: "cancelled"}}})
	}
	return conn.SendResult(&linkv1.TaskResult{TaskId: task.TaskId,
		Result: &linkv1.TaskResult_SendVideo{SendVideo: &linkv1.SendVideoResult{MessageId: 1}}})
}

// TestStopLetsTasksFinish: on SIGTERM the running send completes and its
// result reaches the core before the worker exits (REVIEW M19).
func TestStopLetsTasksFinish(t *testing.T) {
	tests := []struct {
		name    string
		delay   time.Duration
		drain   time.Duration
		want    queue.Status
		maxExit time.Duration
	}{
		{"finishes within the drain", 300 * time.Millisecond, 5 * time.Second, queue.StatusDone, 3 * time.Second},
		{"cancelled after the drain", time.Hour, 300 * time.Millisecond, queue.StatusFailed, 8 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := startCore(t)
			payload, _ := proto.Marshal(&linkv1.SendVideoTask{VideoId: "tiktok:1"})
			id, _ := c.q.Enqueue(context.Background(), queue.EnqueueParams{Kind: queue.KindSendVideo, Payload: payload, MaxAttempts: 1})
			h := &sleepyHandler{delay: tt.delay, started: make(chan struct{})}
			client := workerlink.New(workerlink.Config{WorkerID: "w1", CoreAddr: c.addr, Handler: h, Drain: tt.drain}, quiet, nil)
			stop, sigterm := context.WithCancel(context.Background())
			done := make(chan struct{})
			go func() { client.Run(stop); close(done) }()

			<-h.started
			start := time.Now()
			sigterm()
			select {
			case <-done:
			case <-time.After(tt.maxExit):
				t.Fatal("worker did not exit in time")
			}
			if took := time.Since(start); took < tt.delay && tt.want == queue.StatusDone {
				t.Fatalf("exited after %s, before the task could finish", took)
			}
			waitFor(t, "task status", func() bool {
				task, err := c.q.Get(context.Background(), id)
				return err == nil && task.Status == tt.want
			})
		})
	}
}
