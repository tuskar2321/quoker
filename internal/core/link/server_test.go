package link

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"log/slog"
	"math/big"
	"net"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	linkv1 "github.com/tuskar2321/quoker/gen/quoker/link/v1"
	"github.com/tuskar2321/quoker/internal/core/metrics"
	"github.com/tuskar2321/quoker/internal/core/proxy"
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
	return f.connectWith(workerID)
}

// connectWith is connect with extra metadata pairs (key, value, ...).
func (f *testFixture) connectWith(workerID string, extra ...string) linkv1.LinkService_ConnectClient {
	f.t.Helper()
	return f.connectAs(workerID, "fp-"+workerID, extra...)
}

// connectAs is connectWith with an explicit certificate fingerprint header.
func (f *testFixture) connectAs(workerID, fingerprint string, extra ...string) linkv1.LinkService_ConnectClient {
	f.t.Helper()
	md := metadata.Pairs(append([]string{
		"x-client-cert-fingerprint", fingerprint,
		"x-client-cert-cn", workerID,
	}, extra...)...)
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

// TestRevokeClosesStream: the core ends the stream itself, so a worker that
// ignores Shutdown is still cut off (REVIEW H4).
func TestRevokeClosesStream(t *testing.T) {
	f := newTestFixture(t)
	stream := f.connect("worker-1")
	if err := f.srv.Shutdown(f.ctx, "worker-1", ShutdownRevoked); err != nil {
		t.Fatal(err)
	}
	for {
		_, err := stream.Recv() // a malicious worker would just keep reading
		if err == nil {
			continue
		}
		if status.Code(err) != codes.PermissionDenied {
			t.Fatalf("stream ended with %v, want PermissionDenied", err)
		}
		break
	}
	deadline := time.Now().Add(2 * time.Second)
	for len(f.srv.ConnectedWorkers()) > 0 {
		if time.Now().After(deadline) {
			t.Fatal("revoked worker still registered")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestUntrustedPeerRejected: certificate headers count only from Caddy's
// network; a direct caller cannot pose as a worker (REVIEW L5).
func TestUntrustedPeerRejected(t *testing.T) {
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "q.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	_, _ = st.DB().ExecContext(context.Background(),
		`INSERT INTO workers (id, fingerprint, status, created_at) VALUES ('worker-1', 'fp-worker-1', 'allowed', 0)`)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	onlyCaddy, _ := proxy.Parse("10.9.0.0/16") // the test client comes from 127.0.0.1
	srv := New(ctx, Deps{Store: st, Queue: queue.New(st, nil), Settings: settings.New(st.DB(), nil), Trusted: &onlyCaddy})
	lis, _ := (&net.ListenConfig{}).Listen(ctx, "tcp", "127.0.0.1:0")
	gs := grpc.NewServer()
	linkv1.RegisterLinkServiceServer(gs, srv)
	go func() { _ = gs.Serve(lis) }()
	defer gs.Stop()
	cc, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cc.Close() }()

	md := metadata.Pairs("x-client-cert-fingerprint", "fp-worker-1", "x-client-cert-cn", "worker-1")
	stream, err := linkv1.NewLinkServiceClient(cc).Connect(metadata.NewOutgoingContext(ctx, md))
	if err != nil {
		t.Fatal(err)
	}
	_ = stream.Send(&linkv1.WorkerMessage{Msg: &linkv1.WorkerMessage_Hello{Hello: &linkv1.Hello{WorkerId: "worker-1"}}})
	if _, err := stream.Recv(); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("err = %v, want Unauthenticated", err)
	}
}

// TestReconnectReplacesOldStream: a second stream of the same worker closes
// the first, so two streams never lease for one worker (REVIEW L1).
func TestReconnectReplacesOldStream(t *testing.T) {
	f := newTestFixture(t)
	old := f.connect("worker-1")
	_ = f.connect("worker-1")
	var reason string
	for {
		msg, err := old.Recv()
		if err != nil {
			break
		}
		if sd := msg.GetShutdown(); sd != nil {
			reason = sd.Reason
		}
	}
	if reason != ShutdownReplaced {
		t.Fatalf("old stream closed with %q", reason)
	}
	if got := f.srv.ConnectedWorkers(); len(got) != 1 || got[0] != "worker-1" {
		t.Fatalf("connected = %v", got)
	}
}

// TestHeartbeatUpdatesLastSeen: "last seen" follows heartbeats, not only
// connects (REVIEW L3).
func TestHeartbeatUpdatesLastSeen(t *testing.T) {
	f := newTestFixture(t)
	stream := f.connect("worker-1")
	if _, err := f.srv.st.DB().ExecContext(f.ctx, `UPDATE workers SET last_seen_at = 0 WHERE id = 'worker-1'`); err != nil {
		t.Fatal(err)
	}
	if err := stream.Send(&linkv1.WorkerMessage{Msg: &linkv1.WorkerMessage_Heartbeat{Heartbeat: &linkv1.Heartbeat{}}}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		var seen int64
		_ = f.srv.st.DB().QueryRowContext(f.ctx, `SELECT last_seen_at FROM workers WHERE id = 'worker-1'`).Scan(&seen)
		if seen == f.now.UnixMilli() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("last_seen_at = %d", seen)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestSettingsChangeReachesWorkers: a token written by the CLI (another
// process) reaches connected workers without a reconnect (REVIEW N7).
func TestSettingsChangeReachesWorkers(t *testing.T) {
	f := newTestFixture(t)
	var tokenChanged atomic.Bool
	f.srv.onToken = func() { tokenChanged.Store(true) }
	stream := f.connect("worker-1")
	last, _ := f.srv.settingsStamps(f.ctx)

	if err := f.set.Set(f.ctx, settings.BotToken, "123:NEW-TOKEN"); err != nil {
		t.Fatal(err)
	}
	f.srv.checkSettings(f.ctx, last)
	for {
		msg, err := stream.Recv()
		if err != nil {
			t.Fatal(err)
		}
		if c := msg.GetConfig(); c != nil {
			if c.BotToken != "123:NEW-TOKEN" {
				t.Fatalf("config token = %q", c.BotToken)
			}
			break
		}
	}
	if !tokenChanged.Load() {
		t.Fatal("bot was not told to re-initialise")
	}
}

// TestTaskMetrics: results feed the task duration histogram and the error
// counter (REVIEW N1).
func TestTaskMetrics(t *testing.T) {
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "q.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	_, _ = st.DB().ExecContext(context.Background(),
		`INSERT INTO workers (id, fingerprint, status, created_at) VALUES ('worker-1', 'fp-worker-1', 'allowed', 0)`)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	q := queue.New(st, nil)
	reg := metrics.New()
	srv := New(ctx, Deps{Store: st, Queue: q, Settings: settings.New(st.DB(), nil), Metrics: reg})
	conn := &workerConn{id: "worker-1", out: make(chan *linkv1.CoreMessage, 8), done: make(chan struct{}), closeReq: make(chan string, 1)}

	resolvePayload, _ := marshalProto(&linkv1.ResolveTask{Url: "https://www.tiktok.com/@a/video/1"})
	ok, _ := q.Enqueue(ctx, queue.EnqueueParams{Kind: queue.KindResolve, Payload: resolvePayload})
	_, _ = q.Lease(ctx, "worker-1")
	srv.handleTaskResult(ctx, conn, &linkv1.TaskResult{TaskId: ok,
		Result: &linkv1.TaskResult_Resolve{Resolve: &linkv1.ResolveResult{Platform: "tiktok", NativeId: "1"}}}, srv.log)

	bad, _ := q.Enqueue(ctx, queue.EnqueueParams{Kind: queue.KindResolve, Payload: resolvePayload})
	_, _ = q.Lease(ctx, "worker-1")
	srv.handleTaskResult(ctx, conn, &linkv1.TaskResult{TaskId: bad,
		Result: &linkv1.TaskResult_Error{Error: &linkv1.TaskError{Code: "private_video"}}}, srv.log)

	rec := httptest.NewRecorder()
	reg.Handler(srv.log).ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil)) //nolint:noctx // test
	for _, want := range []string{
		`quoker_task_duration_seconds_count{kind="resolve",platform="tiktok"} 1`,
		`quoker_task_errors_total{kind="resolve",code="private_video"} 1`,
	} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("missing %q in:\n%s", want, rec.Body.String())
		}
	}
}

// testCertDER returns a self-signed certificate that expires at notAfter.
func testCertDER(t *testing.T, cn string, notAfter time.Time) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.CreateCertificate(rand.Reader, &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    notAfter.Add(-365 * 24 * time.Hour),
		NotAfter:     notAfter,
	}, &x509.Certificate{}, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return der
}

// TestCertExpiryRecorded: the client certificate passed by the proxy fills
// workers.cert_not_after (REVIEW N6); a broken header is ignored.
func TestCertExpiryRecorded(t *testing.T) {
	notAfter := time.Date(2027, 1, 2, 3, 4, 5, 0, time.UTC)
	der := testCertDER(t, "worker-1", notAfter)
	other := testCertDER(t, "worker-1", notAfter.AddDate(1, 0, 0))
	sum := sha256.Sum256(der)
	fingerprint := hex.EncodeToString(sum[:])
	tests := []struct {
		name   string
		header string
		want   int64 // 0: column stays NULL
	}{
		{"valid", base64.StdEncoding.EncodeToString(der), notAfter.UnixMilli()},
		{"another certificate", base64.StdEncoding.EncodeToString(other), 0},
		{"not base64", "!!!", 0},
		{"not a certificate", base64.StdEncoding.EncodeToString([]byte("junk")), 0},
		{"absent", "", 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newTestFixture(t)
			if _, err := f.srv.st.DB().ExecContext(f.ctx,
				`UPDATE workers SET fingerprint = ? WHERE id = 'worker-1'`, fingerprint); err != nil {
				t.Fatal(err)
			}
			var extra []string
			if tc.header != "" {
				extra = []string{"x-client-cert-der", tc.header}
			}
			stream := f.connectAs("worker-1", fingerprint, extra...)
			defer func() { _ = stream.CloseSend() }()
			var got sql.NullInt64
			if err := f.srv.st.DB().QueryRowContext(f.ctx,
				`SELECT cert_not_after FROM workers WHERE id = 'worker-1'`).Scan(&got); err != nil {
				t.Fatal(err)
			}
			if tc.want == 0 && got.Valid || tc.want != 0 && got.Int64 != tc.want {
				t.Fatalf("cert_not_after = %+v, want %d", got, tc.want)
			}
		})
	}
}

// TestAdminChatIDInConfig: the admin chat reaches workers in the initial
// Config and in a broadcast when it changes (REVIEW N2).
func TestAdminChatIDInConfig(t *testing.T) {
	f := newTestFixture(t)
	if err := f.set.Set(f.ctx, settings.AdminChatID, "500"); err != nil {
		t.Fatal(err)
	}
	stream := f.connect("worker-1")
	if f.lastConfig.AdminChatId != 500 {
		t.Fatalf("initial admin_chat_id = %d, want 500", f.lastConfig.AdminChatId)
	}
	last, _ := f.srv.settingsStamps(f.ctx)
	if _, err := f.srv.st.DB().ExecContext(f.ctx,
		`UPDATE settings SET value = '777', updated_at = updated_at + 1000 WHERE key = ?`, string(settings.AdminChatID)); err != nil {
		t.Fatal(err)
	}
	f.srv.checkSettings(f.ctx, last)
	for {
		msg, err := stream.Recv()
		if err != nil {
			t.Fatal(err)
		}
		if c := msg.GetConfig(); c != nil {
			if c.AdminChatId != 777 {
				t.Fatalf("broadcast admin_chat_id = %d, want 777", c.AdminChatId)
			}
			return
		}
	}
}
