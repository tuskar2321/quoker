package link

import (
	"context"
	"log/slog"
	"net"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	linkv1 "github.com/tuskar2321/quoker/gen/quoker/link/v1"
	"github.com/tuskar2321/quoker/internal/core/queue"
	"github.com/tuskar2321/quoker/internal/core/settings"
	"github.com/tuskar2321/quoker/internal/core/store"
)

type testFixture struct {
	t       *testing.T
	ctx     context.Context
	cancel  context.CancelFunc
	srv     *Server
	q       *queue.Store
	stub    linkv1.LinkServiceClient
	now     time.Time
	results *fakeResults
	updates *fakeUpdates
	set     *settings.Store

	lastConfig *linkv1.Config
	stopServer context.CancelFunc
}

// fakeUpdates counts handled bot updates.
type fakeUpdates struct {
	mu   sync.Mutex
	seen []string
}

func (u *fakeUpdates) HandleUpdate(_ context.Context, raw []byte) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.seen = append(u.seen, string(raw))
	return nil
}

func (u *fakeUpdates) count() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return len(u.seen)
}

// fakeResults records the calls the server makes to its ResultHandler.
type fakeResults struct {
	mu        sync.Mutex
	succeeded []string
	failed    []string
}

func (r *fakeResults) TaskSucceeded(_ context.Context, t *queue.Task, _ *linkv1.TaskResult) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.succeeded = append(r.succeeded, t.ID)
	return nil
}

func (r *fakeResults) TaskFailed(_ context.Context, t *queue.Task) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.failed = append(r.failed, t.ID)
	return nil
}

func (r *fakeResults) snapshot() (succeeded, failed []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.succeeded...), append([]string(nil), r.failed...)
}

func newTestFixture(t *testing.T) *testFixture {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "q.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	// Insert a test worker into the DB.
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	_, err = st.DB().ExecContext(context.Background(),
		`INSERT INTO workers (id, fingerprint, status, created_at) VALUES (?, ?, 'allowed', ?)`,
		"worker-1", "fp-worker-1", now.UnixMilli())
	if err != nil {
		t.Fatal(err)
	}

	nowFn := func() time.Time { return now }
	q := queue.New(st, nowFn)
	log := slog.New(slog.NewTextHandler(nil, &slog.HandlerOptions{Level: slog.LevelError}))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	srvCtx, stopServer := context.WithCancel(context.Background())
	t.Cleanup(stopServer)

	results := &fakeResults{}
	updates := &fakeUpdates{}
	set := settings.New(st.DB(), nowFn)
	srv := New(srvCtx, Deps{
		Store:    st,
		Queue:    q,
		Settings: set,
		Results:  results,
		Updates:  updates,
		Log:      log,
		Now:      nowFn,
	})

	lis, err := (&net.ListenConfig{}).Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	gs := grpc.NewServer()
	linkv1.RegisterLinkServiceServer(gs, srv)
	go func() {
		_ = gs.Serve(lis)
	}()
	t.Cleanup(func() {
		gs.GracefulStop()
		cancel()
	})

	cc, err := grpc.NewClient(lis.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cc.Close() })

	return &testFixture{
		t: t, ctx: ctx, cancel: cancel,
		srv: srv, q: q,
		stub:       linkv1.NewLinkServiceClient(cc),
		now:        now,
		results:    results,
		updates:    updates,
		set:        set,
		stopServer: stopServer,
	}
}

// connect opens a stream with the cert headers set, sends Hello, and receives
// the initial Config. Returns the stream.
func (f *testFixture) connect(workerID string) linkv1.LinkService_ConnectClient {
	f.t.Helper()
	md := metadata.Pairs(
		"x-client-cert-fingerprint", "fp-"+workerID,
		"x-client-cert-cn", workerID,
	)
	stream, err := f.stub.Connect(metadata.NewOutgoingContext(f.ctx, md))
	if err != nil {
		f.t.Fatal(err)
	}
	// Send Hello.
	if err := stream.Send(&linkv1.WorkerMessage{
		Msg: &linkv1.WorkerMessage_Hello{
			Hello: &linkv1.Hello{WorkerId: workerID, MaxParallel: 2},
		},
	}); err != nil {
		f.t.Fatal(err)
	}
	// Receive Config.
	msg, err := stream.Recv()
	if err != nil {
		f.t.Fatalf("recv config: %v", err)
	}
	if msg.GetConfig() == nil {
		f.t.Fatalf("expected Config, got %T", msg.Msg)
	}
	f.lastConfig = msg.GetConfig()
	return stream
}

func TestConnectAndConfig(t *testing.T) {
	f := newTestFixture(t)
	stream := f.connect("worker-1")
	_ = stream.CloseSend()
}

func TestTaskDispatch(t *testing.T) {
	f := newTestFixture(t)

	// Enqueue a resolve task.
	payload, err := marshalProto(&linkv1.ResolveTask{Url: "https://www.tiktok.com/@a/video/1"})
	if err != nil {
		t.Fatal(err)
	}
	taskID, err := f.q.Enqueue(f.ctx, queue.EnqueueParams{
		Kind:    queue.KindResolve,
		Payload: payload,
	})
	if err != nil {
		t.Fatal(err)
	}

	stream := f.connect("worker-1")

	// Should receive the Task.
	msg, err := stream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	task := msg.GetTask()
	if task == nil {
		t.Fatalf("expected Task, got %T", msg.Msg)
	}
	if task.TaskId != taskID {
		t.Fatalf("task ID = %s, want %s", task.TaskId, taskID)
	}
	if task.GetResolve() == nil {
		t.Fatalf("expected ResolveTask payload")
	}

	// Send a successful result.
	if err := stream.Send(&linkv1.WorkerMessage{
		Msg: &linkv1.WorkerMessage_TaskResult{
			TaskResult: &linkv1.TaskResult{
				TaskId: taskID,
				Result: &linkv1.TaskResult_Resolve{
					Resolve: &linkv1.ResolveResult{Platform: "tiktok", NativeId: "1"},
				},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}

	// Wait for the task to be completed in the queue.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got, err := f.q.Get(f.ctx, taskID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status == queue.StatusDone {
			var res linkv1.ResolveResult
			if err := unmarshalProto(got.Result, &res); err != nil || res.NativeId != "1" {
				t.Fatalf("stored result = %v, err = %v", &res, err)
			}
			if ok, _ := f.results.snapshot(); len(ok) != 1 || ok[0] != taskID {
				t.Fatalf("TaskSucceeded calls = %v", ok)
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("task not completed within deadline")
}

func TestTaskFinalFailureNotifies(t *testing.T) {
	f := newTestFixture(t)
	payload, err := marshalProto(&linkv1.SendVideoTask{VideoId: "tiktok:1", ChatId: -1})
	if err != nil {
		t.Fatal(err)
	}
	taskID, err := f.q.Enqueue(f.ctx, queue.EnqueueParams{Kind: queue.KindSendVideo, Payload: payload})
	if err != nil {
		t.Fatal(err)
	}
	stream := f.connect("worker-1")
	if _, err := stream.Recv(); err != nil {
		t.Fatal(err)
	}
	if err := stream.Send(&linkv1.WorkerMessage{
		Msg: &linkv1.WorkerMessage_TaskResult{TaskResult: &linkv1.TaskResult{
			TaskId: taskID,
			Result: &linkv1.TaskResult_Error{Error: &linkv1.TaskError{Code: "target_forbidden"}},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		ok, failed := f.results.snapshot()
		if len(failed) == 1 && failed[0] == taskID {
			if len(ok) != 0 {
				t.Fatalf("unexpected TaskSucceeded calls: %v", ok)
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("TaskFailed not called within deadline")
}

func TestUnknownWorker(t *testing.T) {
	f := newTestFixture(t)
	md := metadata.Pairs(
		"x-client-cert-fingerprint", "fp-unknown",
		"x-client-cert-cn", "unknown",
	)
	stream, err := f.stub.Connect(metadata.NewOutgoingContext(f.ctx, md))
	if err != nil {
		t.Fatal(err)
	}
	// Send Hello; the server should close with PermissionDenied.
	_ = stream.Send(&linkv1.WorkerMessage{
		Msg: &linkv1.WorkerMessage_Hello{
			Hello: &linkv1.Hello{WorkerId: "unknown"},
		},
	})
	// Either connect fails or first Recv gives an error.
	_, err = stream.Recv()
	if err == nil {
		t.Fatal("expected error for unknown worker")
	}
}

func TestBotUpdatesProcessedOnceAndAcked(t *testing.T) {
	f := newTestFixture(t)
	stream := f.connect("worker-1")
	if !f.lastConfig.PollUpdates {
		t.Fatal("the first worker should become the poller")
	}
	for range 2 { // the poller re-sends when an ack is lost
		if err := stream.Send(&linkv1.WorkerMessage{Msg: &linkv1.WorkerMessage_BotUpdate{
			BotUpdate: &linkv1.BotUpdate{UpdateId: 5, UpdateJson: `{"update_id":5}`},
		}}); err != nil {
			t.Fatal(err)
		}
	}
	acks := 0
	for acks < 2 {
		msg, err := stream.Recv()
		if err != nil {
			t.Fatal(err)
		}
		if a := msg.GetBotUpdateAck(); a != nil {
			if a.UpdateId != 5 {
				t.Fatalf("ack for %d", a.UpdateId)
			}
			acks++
		}
	}
	if n := f.updates.count(); n != 1 {
		t.Fatalf("handled %d times, want 1", n)
	}
}

func TestPollerHandover(t *testing.T) {
	f := newTestFixture(t)
	if _, err := f.srv.st.DB().ExecContext(f.ctx,
		`INSERT INTO workers (id, fingerprint, status, created_at) VALUES ('worker-2', 'fp-worker-2', 'allowed', 0)`); err != nil {
		t.Fatal(err)
	}
	// worker-1 polled before a restart and has not come back.
	if err := f.set.Set(f.ctx, settings.PollerWorkerID, "worker-1"); err != nil {
		t.Fatal(err)
	}
	stream := f.connect("worker-2")
	if f.lastConfig.PollUpdates {
		t.Fatal("worker-2 took over without waiting for the grace period")
	}
	clock := f.now
	f.srv.now = func() time.Time { return clock }
	f.srv.checkPoller(f.ctx) // poller noticed missing
	clock = clock.Add(pollerGrace + time.Second)
	f.srv.checkPoller(f.ctx)

	if cur, _, _ := f.set.Get(f.ctx, settings.PollerWorkerID); cur != "worker-2" {
		t.Fatalf("poller = %q, want worker-2", cur)
	}
	for {
		msg, err := stream.Recv()
		if err != nil {
			t.Fatal(err)
		}
		if c := msg.GetConfig(); c != nil {
			if !c.PollUpdates {
				t.Fatal("new config does not enable polling")
			}
			return
		}
	}
}

func TestPurgeUpdates(t *testing.T) {
	f := newTestFixture(t)
	old := f.now.Add(-8 * 24 * time.Hour).UnixMilli()
	if _, err := f.srv.st.DB().ExecContext(f.ctx,
		`INSERT INTO bot_updates (update_id, processed_at) VALUES (1, ?), (2, ?)`, old, f.now.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if err := f.srv.purgeUpdates(f.ctx); err != nil {
		t.Fatal(err)
	}
	var ids []int64
	rows, err := f.srv.st.DB().QueryContext(f.ctx, `SELECT update_id FROM bot_updates`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id int64
		_ = rows.Scan(&id)
		ids = append(ids, id)
	}
	if len(ids) != 1 || ids[0] != 2 {
		t.Fatalf("left = %v", ids)
	}
}

// TestCoreShutdownClosesStreams: stopping the core sends Shutdown and ends
// every stream, so grpc.Server.GracefulStop can return (REVIEW H3).
func TestCoreShutdownClosesStreams(t *testing.T) {
	f := newTestFixture(t)
	stream := f.connect("worker-1")
	f.stopServer()
	var reason string
	for {
		msg, err := stream.Recv()
		if err != nil {
			break
		}
		if sd := msg.GetShutdown(); sd != nil {
			reason = sd.Reason
		}
	}
	if reason != ShutdownCoreStopping {
		t.Fatalf("shutdown reason = %q", reason)
	}
}

// recvTask reads messages until a Task arrives.
func recvTask(t *testing.T, stream linkv1.LinkService_ConnectClient) *linkv1.Task {
	t.Helper()
	for {
		msg, err := stream.Recv()
		if err != nil {
			t.Fatal(err)
		}
		if task := msg.GetTask(); task != nil {
			return task
		}
	}
}

func waitStatus(t *testing.T, f *testFixture, id string, want queue.Status) *queue.Task {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		got, err := f.q.Get(f.ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status == want {
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("task %s status = %s, want %s", id, got.Status, want)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestDisconnectReleasesTasks: a dropped stream gives resolve back to the
// queue at once, but keeps send_video with the worker (REVIEW H6).
func TestDisconnectReleasesTasks(t *testing.T) {
	f := newTestFixture(t)
	resolvePayload, _ := marshalProto(&linkv1.ResolveTask{Url: "u"})
	sendPayload, _ := marshalProto(&linkv1.SendVideoTask{VideoId: "tiktok:1"})
	resolveID, _ := f.q.Enqueue(f.ctx, queue.EnqueueParams{Kind: queue.KindResolve, Payload: resolvePayload})
	sendID, _ := f.q.Enqueue(f.ctx, queue.EnqueueParams{Kind: queue.KindSendVideo, Payload: sendPayload})

	streamCtx, drop := context.WithCancel(f.ctx)
	md := metadata.Pairs("x-client-cert-fingerprint", "fp-worker-1", "x-client-cert-cn", "worker-1")
	stream, err := f.stub.Connect(metadata.NewOutgoingContext(streamCtx, md))
	if err != nil {
		t.Fatal(err)
	}
	_ = stream.Send(&linkv1.WorkerMessage{Msg: &linkv1.WorkerMessage_Hello{Hello: &linkv1.Hello{WorkerId: "worker-1", MaxParallel: 2}}})
	recvTask(t, stream)
	recvTask(t, stream)
	drop()

	waitStatus(t, f, resolveID, queue.StatusQueued)
	if got, _ := f.q.Get(f.ctx, sendID); got.Status != queue.StatusLeased {
		t.Fatalf("send_video = %s, want still leased", got.Status)
	}
}

// TestLateSendResultAccepted: a send_video result that arrives after the
// task was requeued still counts, so the video is not posted twice (REVIEW H5).
func TestLateSendResultAccepted(t *testing.T) {
	f := newTestFixture(t)
	payload, _ := marshalProto(&linkv1.SendVideoTask{VideoId: "tiktok:1"})
	id, _ := f.q.Enqueue(f.ctx, queue.EnqueueParams{Kind: queue.KindSendVideo, Payload: payload})
	if _, err := f.q.Lease(f.ctx, "worker-old"); err != nil {
		t.Fatal(err)
	}
	// The lease expired and the task went back to the queue.
	if _, err := f.srv.st.DB().ExecContext(f.ctx,
		`UPDATE tasks SET status = 'queued', worker_id = NULL, not_before = ? WHERE id = ?`,
		f.now.Add(time.Hour).UnixMilli(), id); err != nil {
		t.Fatal(err)
	}
	stream := f.connect("worker-1")
	if err := stream.Send(&linkv1.WorkerMessage{Msg: &linkv1.WorkerMessage_TaskResult{TaskResult: &linkv1.TaskResult{
		TaskId: id, Result: &linkv1.TaskResult_SendVideo{SendVideo: &linkv1.SendVideoResult{MessageId: 7}},
	}}}); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, f, id, queue.StatusDone)
	if ok, _ := f.results.snapshot(); len(ok) != 1 || ok[0] != id {
		t.Fatalf("TaskSucceeded calls = %v", ok)
	}
}
