package link

import (
	"context"
	"log/slog"
	"net"
	"path/filepath"
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
	t      *testing.T
	ctx    context.Context
	cancel context.CancelFunc
	srv    *Server
	q      *queue.Store
	stub   linkv1.LinkServiceClient
	now    time.Time
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

	srv := New(ctx, Deps{
		Store:    st,
		Queue:    q,
		Settings: settings.New(st.DB(), nowFn),
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
		stub: linkv1.NewLinkServiceClient(cc),
		now:  now,
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
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("task not completed within deadline")
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
