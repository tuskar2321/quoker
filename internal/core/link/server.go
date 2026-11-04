// Package link implements the gRPC server that worker processes connect to.
// See docs/04-core-worker-protocol.md and proto/quoker/link/v1/link.proto.
package link

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	linkv1 "github.com/tuskar2321/quoker/gen/quoker/link/v1"
	"github.com/tuskar2321/quoker/internal/core/queue"
	"github.com/tuskar2321/quoker/internal/core/settings"
	"github.com/tuskar2321/quoker/internal/core/store"
)

const (
	// heartbeatTimeout is how long we wait for a heartbeat before treating the
	// stream as dead (docs/04-core-worker-protocol.md §3).
	heartbeatTimeout = 45 * time.Second
	// heartbeatInterval is how often the server sends heartbeats.
	heartbeatInterval = 15 * time.Second
	// leaseRecoveryInterval is how often we re-queue stalled tasks.
	leaseRecoveryInterval = 30 * time.Second
	// queuedTimeout is how long a ready task may wait while no worker is
	// connected before it fails with worker_unavailable (docs/02).
	queuedTimeout = 10 * time.Minute
	// updateTimeout bounds processing of one bot update.
	updateTimeout = 2 * time.Minute
	// pollerGrace is how long the poller may be gone before another worker
	// takes over getUpdates (docs/04-core-worker-protocol.md).
	pollerGrace = 30 * time.Second
	pollerCheck = 5 * time.Second
	// processedUpdatesTTL is how long update ids are kept for dedup
	// (docs/05-data-model.md); Telegram never re-sends older ones.
	processedUpdatesTTL = 7 * 24 * time.Hour
	purgeInterval       = time.Hour
	// headerFingerprint is set by Caddy after mTLS verification.
	headerFingerprint = "X-Client-Cert-Fingerprint"
	// headerCertCN is set by Caddy to the certificate's Common Name.
	headerCertCN = "X-Client-Cert-CN"
)

// ResultHandler applies task outcomes to domain state.
type ResultHandler interface {
	// TaskSucceeded runs before the task is marked done, so readers that see
	// status=done also see the applied result.
	TaskSucceeded(ctx context.Context, t *queue.Task, r *linkv1.TaskResult) error
	// TaskFailed runs once a task has failed with no retries left.
	TaskFailed(ctx context.Context, t *queue.Task) error
}

// Deps are the dependencies of the link server.
type Deps struct {
	Store    *store.Store
	Queue    *queue.Store
	Settings *settings.Store
	Results  ResultHandler // nil ignores results
	Updates  UpdateHandler // nil drops bot updates
	// UploadBase is sent to workers as Config.upload_base.
	UploadBase string
	Log        *slog.Logger
	Now        func() time.Time
}

// ShutdownCoreStopping is the Shutdown reason sent when the core stops; the
// worker finishes its tasks and reconnects later.
const ShutdownCoreStopping = "core_stopping"

// UpdateHandler processes one Telegram update forwarded by the poller.
type UpdateHandler interface {
	HandleUpdate(ctx context.Context, updateJSON []byte) error
}

// UpdateFunc adapts a function to UpdateHandler.
type UpdateFunc func(ctx context.Context, updateJSON []byte) error

// HandleUpdate calls f.
func (f UpdateFunc) HandleUpdate(ctx context.Context, updateJSON []byte) error {
	return f(ctx, updateJSON)
}

// Server is the gRPC LinkService implementation.
type Server struct {
	linkv1.UnimplementedLinkServiceServer
	st      *store.Store
	q       *queue.Store
	set     *settings.Store
	results ResultHandler
	updates UpdateHandler
	upload  string
	log     *slog.Logger
	now     func() time.Time

	updateCh chan pendingUpdate
	stopping <-chan struct{} // closed when the core shuts down

	mu           sync.Mutex
	workers      map[string]*workerConn // keyed by worker_id
	pollerLostAt time.Time              // when the poller was last seen missing
}

// New returns a Server. It also starts background goroutines for lease
// recovery and config broadcasts; use a context to stop them.
func New(ctx context.Context, d Deps) *Server {
	if d.Results == nil {
		d.Results = noopResults{}
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Log == nil {
		d.Log = slog.Default()
	}
	srv := &Server{
		st:       d.Store,
		q:        d.Queue,
		set:      d.Settings,
		results:  d.Results,
		updates:  d.Updates,
		upload:   d.UploadBase,
		log:      d.Log,
		now:      d.Now,
		updateCh: make(chan pendingUpdate, 256),
		stopping: ctx.Done(),
		workers:  make(map[string]*workerConn),
	}
	go srv.leaseRecoveryLoop(ctx)
	go srv.updateLoop(ctx)
	go srv.purgeLoop(ctx)
	go srv.pollerLoop(ctx)
	return srv
}

// workerConn tracks an active worker stream. Every outgoing message goes
// through out, drained by a single sender: gRPC streams forbid concurrent Send.
type workerConn struct {
	id          string
	out         chan *linkv1.CoreMessage
	maxParallel int
	done        chan struct{} // closed when the stream ends
}

// send queues msg; it reports false once the stream has ended.
func (c *workerConn) send(msg *linkv1.CoreMessage) bool {
	select {
	case c.out <- msg:
		return true
	case <-c.done:
		return false
	}
}

// Connect implements LinkServiceServer.
func (s *Server) Connect(stream linkv1.LinkService_ConnectServer) error {
	fingerprint, _, err := s.certInfo(stream.Context())
	if err != nil {
		return status.Error(codes.Unauthenticated, err.Error())
	}
	workerID, err := s.VerifyWorker(stream.Context(), fingerprint)
	if err != nil {
		return status.Error(codes.PermissionDenied, err.Error())
	}
	log := s.log.With("worker_id", workerID)

	msg, err := stream.Recv()
	if err != nil {
		return status.Errorf(codes.Internal, "recv hello: %v", err)
	}
	hello := msg.GetHello()
	if hello == nil {
		return status.Error(codes.InvalidArgument, "first message must be Hello")
	}
	if hello.WorkerId != workerID {
		return status.Errorf(codes.InvalidArgument, "worker_id %q must match certificate CN %q",
			hello.WorkerId, workerID)
	}
	maxParallel := int(hello.MaxParallel)
	if maxParallel <= 0 {
		maxParallel = 2
	}

	conn := &workerConn{
		id:          workerID,
		out:         make(chan *linkv1.CoreMessage, 32),
		maxParallel: maxParallel,
		done:        make(chan struct{}),
	}
	s.registerWorker(conn)
	defer func() {
		if s.unregisterWorker(conn) {
			s.releaseTasks(workerID, log)
		}
	}()
	s.updateWorkerSeen(stream.Context(), workerID, hello)
	s.claimPollerIfFree(stream.Context(), workerID)

	cfg, err := s.loadConfig(stream.Context(), workerID)
	if err != nil {
		return status.Errorf(codes.Internal, "load config: %v", err)
	}
	conn.send(&linkv1.CoreMessage{Msg: &linkv1.CoreMessage_Config{Config: cfg}})
	log.Info("worker connected", "max_parallel", maxParallel, "version", hello.Version, "poller", cfg.PollUpdates)

	ctx, cancel := context.WithCancel(stream.Context())
	defer cancel()

	// The sender has its own context so that on shutdown it can be stopped
	// first, leaving Connect as the only writer for the final Shutdown.
	sendCtx, stopSender := context.WithCancel(ctx)
	defer stopSender()
	sendErr := make(chan error, 1)
	go func() { sendErr <- s.senderLoop(sendCtx, conn, stream) }()
	go s.dispatchLoop(ctx, conn)

	type received struct {
		msg *linkv1.WorkerMessage
		err error
	}
	recvCh := make(chan received, 1)
	go func() {
		for {
			m, err := stream.Recv()
			select {
			case recvCh <- received{m, err}:
			case <-ctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()

	hbTick := time.NewTicker(heartbeatInterval)
	defer hbTick.Stop()
	hbDeadline := time.NewTimer(heartbeatTimeout)
	defer hbDeadline.Stop()

	for {
		select {
		case err := <-sendErr:
			if err != nil {
				log.Info("worker send error", "err", err)
			}
			return err
		case <-hbTick.C:
			conn.send(&linkv1.CoreMessage{Msg: &linkv1.CoreMessage_Heartbeat{
				Heartbeat: &linkv1.Heartbeat{UnixMs: s.now().UnixMilli()},
			}})
		case <-hbDeadline.C:
			log.Warn("heartbeat timeout, closing stream")
			return status.Error(codes.DeadlineExceeded, "heartbeat timeout")
		case <-s.stopping:
			stopSender()
			if err := <-sendErr; err != nil {
				log.Debug("sender stopped with error", "err", err)
			}
			_ = stream.Send(&linkv1.CoreMessage{Msg: &linkv1.CoreMessage_Shutdown{
				Shutdown: &linkv1.Shutdown{Reason: ShutdownCoreStopping},
			}})
			log.Info("stream closed: core is shutting down")
			return status.Error(codes.Unavailable, "core is shutting down")
		case r := <-recvCh:
			if r.err != nil {
				return r.err
			}
			hbDeadline.Reset(heartbeatTimeout)
			s.handleWorkerMsg(ctx, conn, r.msg, log)
		}
	}
}

func (s *Server) handleWorkerMsg(ctx context.Context, conn *workerConn, msg *linkv1.WorkerMessage, log *slog.Logger) {
	switch v := msg.Msg.(type) {
	case *linkv1.WorkerMessage_TaskProgress:
		if err := s.q.SetProgress(ctx, v.TaskProgress.TaskId, conn.id, int(v.TaskProgress.Percent)); err != nil {
			log.Warn("set progress failed", "task", v.TaskProgress.TaskId, "err", err)
		}

	case *linkv1.WorkerMessage_TaskResult:
		if v.TaskResult.GetDownload() != nil {
			// Verifying a download hashes up to 500 MB; keep the stream's
			// receive loop (heartbeats, bot_call results) moving meanwhile.
			go s.handleTaskResult(context.WithoutCancel(ctx), conn, v.TaskResult, log)
			return
		}
		s.handleTaskResult(ctx, conn, v.TaskResult, log)

	case *linkv1.WorkerMessage_BotUpdate:
		select {
		case s.updateCh <- pendingUpdate{conn: conn, update: v.BotUpdate}:
		default:
			// Never block the receive loop: the update handler may be waiting
			// for a bot_call result on this very stream. Without an ack the
			// poller sends the update again.
			log.Warn("bot update dropped, backlog full", "update_id", v.BotUpdate.UpdateId)
		}
	}
}

func (s *Server) handleTaskResult(ctx context.Context, conn *workerConn, r *linkv1.TaskResult, log *slog.Logger) {
	task, err := s.q.Get(ctx, r.TaskId)
	if err != nil {
		log.Warn("task result for unknown task", "task_id", r.TaskId, "err", err)
		return
	}
	mine := task.Status == queue.StatusLeased && task.WorkerID == conn.id

	if e := r.GetError(); e != nil {
		if !mine {
			log.Info("stale task error ignored", "task_id", r.TaskId, "status", task.Status)
			return
		}
		s.complete(ctx, queue.CompleteParams{
			TaskID: r.TaskId, WorkerID: conn.id, ErrCode: e.Code, ErrMsg: e.Message, Retryable: e.Retryable,
		}, log)
		log.Info("task failed", "task_id", r.TaskId, "kind", task.Kind, "error_code", e.Code)
		return
	}
	if task.Status != queue.StatusLeased && task.Status != queue.StatusQueued {
		log.Info("result for finished task ignored", "task_id", r.TaskId, "status", task.Status)
		return
	}
	result, err := resultPayload(r)
	if err != nil {
		log.Warn("bad task result", "task_id", r.TaskId, "err", err)
		return
	}
	if err := s.results.TaskSucceeded(ctx, task, r); err != nil {
		log.Error("apply task result", "task_id", r.TaskId, "kind", task.Kind, "err", err)
		// Retrying is safe for everything but send_video: the video is
		// already in the chat, a retry would post it twice.
		if task.Kind != queue.KindSendVideo && mine {
			s.complete(ctx, queue.CompleteParams{
				TaskID: r.TaskId, WorkerID: conn.id, ErrCode: "internal", ErrMsg: "apply result: " + err.Error(), Retryable: true,
			}, log)
			return
		}
	}
	if mine {
		s.complete(ctx, queue.CompleteParams{TaskID: r.TaskId, WorkerID: conn.id, Result: result}, log)
		log.Info("task done", "task_id", r.TaskId, "kind", task.Kind)
		return
	}
	// A result delivered after a reconnect: the work is done, keep it and
	// stop any second run (for send_video this is what prevents a duplicate).
	prev, err := s.q.CompleteLate(ctx, r.TaskId, result)
	if err != nil {
		log.Info("late result not recorded", "task_id", r.TaskId, "err", err)
		return
	}
	if prev != "" && prev != conn.id {
		s.CancelTask(prev, r.TaskId)
	}
	log.Info("late task result accepted", "task_id", r.TaskId, "kind", task.Kind, "rerun_on", prev)
}

// complete marks a task finished and notifies the result handler when the
// task has failed for good.
func (s *Server) complete(ctx context.Context, p queue.CompleteParams, log *slog.Logger) {
	if err := s.q.Complete(ctx, p); err != nil {
		log.Warn("complete task", "task_id", p.TaskID, "err", err)
		return
	}
	if p.ErrCode != "" {
		s.notifyIfFailed(ctx, p.TaskID, log)
	}
}

func (s *Server) notifyIfFailed(ctx context.Context, taskID string, log *slog.Logger) {
	t, err := s.q.Get(ctx, taskID)
	if err != nil {
		log.Warn("reload task", "task_id", taskID, "err", err)
		return
	}
	if t.Status != queue.StatusFailed {
		return
	}
	if err := s.results.TaskFailed(ctx, t); err != nil {
		log.Error("apply task failure", "task_id", taskID, "kind", t.Kind, "err", err)
	}
}

// resultPayload encodes the kind-specific result stored in tasks.result.
func resultPayload(r *linkv1.TaskResult) ([]byte, error) {
	var m proto.Message
	switch v := r.Result.(type) {
	case *linkv1.TaskResult_Resolve:
		m = v.Resolve
	case *linkv1.TaskResult_Download:
		m = v.Download
	case *linkv1.TaskResult_SendVideo:
		m = v.SendVideo
	case *linkv1.TaskResult_BotCall:
		m = v.BotCall
	default:
		return nil, fmt.Errorf("task %s: empty result", r.TaskId)
	}
	return marshalProto(m)
}

type noopResults struct{}

func (noopResults) TaskSucceeded(context.Context, *queue.Task, *linkv1.TaskResult) error {
	return nil
}
func (noopResults) TaskFailed(context.Context, *queue.Task) error { return nil }

type pendingUpdate struct {
	conn   *workerConn
	update *linkv1.BotUpdate
}

// updateLoop processes bot updates one at a time, in arrival order, away from
// the stream's receive loop: handlers may wait for bot_call results that
// arrive on that same stream. The ack goes out only after processing, so the
// poller re-delivers anything the core did not finish.
func (s *Server) updateLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case p := <-s.updateCh:
			if err := s.processUpdate(ctx, p.update); err != nil {
				s.log.Warn("bot update not processed", "update_id", p.update.UpdateId, "err", err)
				continue // no ack: the poller will send it again
			}
			p.conn.send(&linkv1.CoreMessage{Msg: &linkv1.CoreMessage_BotUpdateAck{
				BotUpdateAck: &linkv1.BotUpdateAck{UpdateId: p.update.UpdateId},
			}})
		}
	}
}

func (s *Server) processUpdate(ctx context.Context, u *linkv1.BotUpdate) error {
	var seen int
	err := s.st.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM bot_updates WHERE update_id = ?`, u.UpdateId).Scan(&seen)
	if err != nil {
		return fmt.Errorf("check bot update: %w", err)
	}
	if seen > 0 {
		return nil
	}
	if s.updates != nil {
		hctx, cancel := context.WithTimeout(ctx, updateTimeout)
		err := s.updates.HandleUpdate(hctx, []byte(u.UpdateJson))
		cancel()
		if err != nil {
			// Logged and dropped: retrying a broken update forever would
			// block every update behind it.
			s.log.Error("handle bot update", "update_id", u.UpdateId, "err", err)
		}
	}
	_, err = s.st.DB().ExecContext(ctx,
		`INSERT OR IGNORE INTO bot_updates (update_id, processed_at) VALUES (?, ?)`,
		u.UpdateId, s.now().UnixMilli())
	if err != nil {
		return fmt.Errorf("record bot update: %w", err)
	}
	return nil
}

// senderLoop is the only writer of the stream.
func (s *Server) senderLoop(ctx context.Context, conn *workerConn, stream linkv1.LinkService_ConnectServer) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case m := <-conn.out:
			if err := stream.Send(m); err != nil {
				return err
			}
		}
	}
}

// dispatchLoop picks tasks from the queue and sends them to the worker.
func (s *Server) dispatchLoop(ctx context.Context, conn *workerConn) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			running, err := s.q.LeasedCount(ctx, conn.id)
			if err != nil {
				s.log.Warn("count leased", "worker", conn.id, "err", err)
				continue
			}
			for running < conn.maxParallel {
				task, err := s.q.Lease(ctx, conn.id)
				if errors.Is(err, queue.ErrNoTask) {
					break
				}
				if err != nil {
					s.log.Warn("lease task", "worker", conn.id, "err", err)
					break
				}
				protoTask, err := s.buildProtoTask(ctx, task)
				if err != nil {
					s.log.Warn("build proto task", "task", task.ID, "err", err)
					s.complete(ctx, queue.CompleteParams{
						TaskID: task.ID, WorkerID: conn.id,
						ErrCode: "internal", ErrMsg: err.Error(), Retryable: true,
					}, s.log)
					break
				}
				running++
				if !conn.send(&linkv1.CoreMessage{Msg: &linkv1.CoreMessage_Task{Task: protoTask}}) {
					return
				}
			}
		}
	}
}

// buildProtoTask encodes a queue.Task into the protobuf Task message.
func (s *Server) buildProtoTask(_ context.Context, t *queue.Task) (*linkv1.Task, error) {
	leaseMs := int64(defaultLease(t.Kind) / time.Millisecond)
	proto := &linkv1.Task{
		TaskId:  t.ID,
		LeaseMs: leaseMs,
	}

	// Decode payload (protobuf) per kind. For now we pass the raw bytes in a
	// switch; full decode happens when the videos/sends packages exist.
	switch t.Kind {
	case queue.KindResolve:
		var p linkv1.ResolveTask
		if err := unmarshalProto(t.Payload, &p); err != nil {
			return nil, fmt.Errorf("decode resolve payload: %w", err)
		}
		proto.Kind = &linkv1.Task_Resolve{Resolve: &p}
	case queue.KindDownload:
		var p linkv1.DownloadTask
		if err := unmarshalProto(t.Payload, &p); err != nil {
			return nil, fmt.Errorf("decode download payload: %w", err)
		}
		proto.Kind = &linkv1.Task_Download{Download: &p}
	case queue.KindSendVideo:
		var p linkv1.SendVideoTask
		if err := unmarshalProto(t.Payload, &p); err != nil {
			return nil, fmt.Errorf("decode send_video payload: %w", err)
		}
		proto.Kind = &linkv1.Task_SendVideo{SendVideo: &p}
	case queue.KindBotCall:
		var p linkv1.BotCallTask
		if err := unmarshalProto(t.Payload, &p); err != nil {
			return nil, fmt.Errorf("decode bot_call payload: %w", err)
		}
		proto.Kind = &linkv1.Task_BotCall{BotCall: &p}
	default:
		return nil, fmt.Errorf("unknown task kind: %s", t.Kind)
	}
	return proto, nil
}

// defaultLease returns the lease duration for a queue.Kind (re-exported for
// proto task building without importing queue internals).
func defaultLease(k queue.Kind) time.Duration {
	switch k {
	case queue.KindResolve:
		return 60 * time.Second
	case queue.KindDownload, queue.KindSendVideo:
		return 10 * time.Minute
	case queue.KindBotCall:
		return 30 * time.Second
	}
	return 60 * time.Second
}

// loadConfig reads settings and builds a Config for the worker.
func (s *Server) loadConfig(ctx context.Context, workerID string) (*linkv1.Config, error) {
	botToken, _, _ := s.set.Get(ctx, settings.BotToken)
	cookies, _, _ := s.set.Get(ctx, settings.InstagramCookies)
	pollerID, _, _ := s.set.Get(ctx, settings.PollerWorkerID)
	return &linkv1.Config{
		BotToken:         botToken,
		InstagramCookies: cookies,
		PollUpdates:      pollerID == workerID,
		MaxParallel:      2,
		MaxSendBytes:     50 << 20,
		MaxDownloadBytes: 500 << 20,
		BotApiBase:       "https://api.telegram.org",
		UploadBase:       s.upload,
	}, nil
}

func (s *Server) registerWorker(conn *workerConn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.workers[conn.id] = conn
}

// unregisterWorker reports whether conn was still the worker's current
// stream (a reconnect may already have replaced it).
func (s *Server) unregisterWorker(conn *workerConn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	close(conn.done)
	if s.workers[conn.id] != conn {
		return false
	}
	delete(s.workers, conn.id)
	return true
}

// releaseTasks requeues what a disconnected worker held. send_video stays
// leased until its lease expires: the worker keeps running it and delivers
// the result when it reconnects.
func (s *Server) releaseTasks(workerID string, log *slog.Logger) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	released, failed, err := s.q.ReleaseWorker(ctx, workerID, queue.KindSendVideo)
	if err != nil {
		log.Warn("release tasks of disconnected worker", "err", err)
		return
	}
	if released+len(failed) > 0 {
		log.Info("tasks of disconnected worker released", "requeued", released, "failed", len(failed))
	}
	for _, id := range failed {
		s.notifyIfFailed(ctx, id, log)
	}
}

// SendConfig pushes a fresh Config to a connected worker (after settings
// change); a disconnected worker gets it on its next connect.
func (s *Server) SendConfig(ctx context.Context, workerID string) error {
	conn, ok := s.conn(workerID)
	if !ok {
		return nil
	}
	cfg, err := s.loadConfig(ctx, workerID)
	if err != nil {
		return err
	}
	conn.send(&linkv1.CoreMessage{Msg: &linkv1.CoreMessage_Config{Config: cfg}})
	return nil
}

// BroadcastConfig pushes a fresh Config to every connected worker.
func (s *Server) BroadcastConfig(ctx context.Context) error {
	var errs []error
	for _, id := range s.ConnectedWorkers() {
		errs = append(errs, s.SendConfig(ctx, id))
	}
	return errors.Join(errs...)
}

// ConnectedWorkers returns the ids of workers with a live stream, sorted.
func (s *Server) ConnectedWorkers() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]string, 0, len(s.workers))
	for id := range s.workers {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

func (s *Server) conn(workerID string) (*workerConn, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.workers[workerID]
	return c, ok
}

// CancelTask asks the worker holding the task to stop it.
func (s *Server) CancelTask(workerID, taskID string) {
	if conn, ok := s.conn(workerID); ok {
		conn.send(&linkv1.CoreMessage{Msg: &linkv1.CoreMessage_CancelTask{CancelTask: &linkv1.CancelTask{TaskId: taskID}}})
	}
}

// Shutdown asks a worker to finish its tasks and exit.
func (s *Server) Shutdown(_ context.Context, workerID, reason string) error {
	if conn, ok := s.conn(workerID); ok {
		conn.send(&linkv1.CoreMessage{Msg: &linkv1.CoreMessage_Shutdown{Shutdown: &linkv1.Shutdown{Reason: reason}}})
	}
	return nil
}

func (s *Server) leaseRecoveryLoop(ctx context.Context) {
	ticker := time.NewTicker(leaseRecoveryInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			rec, failed, err := s.q.RecoverExpiredLeases(ctx)
			if err != nil {
				s.log.Warn("lease recovery", "err", err)
				continue
			}
			if rec+len(failed) > 0 {
				s.log.Info("lease recovery", "recovered", rec, "failed", len(failed))
			}
			for _, id := range failed {
				s.notifyIfFailed(ctx, id, s.log)
			}
			if len(s.ConnectedWorkers()) == 0 {
				expired, err := s.q.ExpireQueued(ctx, s.now().Add(-queuedTimeout))
				if err != nil {
					s.log.Warn("expire queued tasks", "err", err)
				}
				for _, id := range expired {
					s.notifyIfFailed(ctx, id, s.log)
				}
				if len(expired) > 0 {
					s.log.Warn("tasks failed: no worker connected", "count", len(expired))
				}
			}
		}
	}
}

// VerifyWorker resolves a certificate fingerprint to an allowed worker id.
func (s *Server) VerifyWorker(ctx context.Context, fingerprint string) (string, error) {
	var id, dbStatus string
	err := s.st.DB().QueryRowContext(ctx,
		`SELECT id, status FROM workers WHERE fingerprint = ?`, fingerprint).Scan(&id, &dbStatus)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("unknown worker fingerprint %s", fingerprint)
	}
	if err != nil {
		return "", fmt.Errorf("lookup worker: %w", err)
	}
	if dbStatus != "allowed" {
		return "", fmt.Errorf("worker %s is %s", id, dbStatus)
	}
	return id, nil
}

// updateWorkerSeen updates worker version and last_seen_at.
func (s *Server) updateWorkerSeen(ctx context.Context, workerID string, hello *linkv1.Hello) {
	_, err := s.st.DB().ExecContext(ctx,
		`UPDATE workers SET version = ?, ytdlp_version = ?, last_seen_at = ? WHERE id = ?`,
		hello.Version, hello.YtdlpVersion, s.now().UnixMilli(), workerID)
	if err != nil {
		s.log.Warn("update worker seen", "worker", workerID, "err", err)
	}
}

// certInfo extracts the client cert fingerprint and CN from gRPC metadata.
// In production these are injected by Caddy; in tests they can be set directly.
func (s *Server) certInfo(ctx context.Context) (fingerprint, cn string, err error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", "", fmt.Errorf("missing metadata")
	}
	fps := md.Get(headerFingerprint)
	cns := md.Get(headerCertCN)
	if len(fps) == 0 || fps[0] == "" {
		return "", "", fmt.Errorf("missing %s header", headerFingerprint)
	}
	if len(cns) == 0 || cns[0] == "" {
		return "", "", fmt.Errorf("missing %s header", headerCertCN)
	}
	return fps[0], cns[0], nil
}

// GRPCServer creates a configured *grpc.Server for the link service.
func GRPCServer(opts ...grpc.ServerOption) *grpc.Server {
	return grpc.NewServer(opts...)
}

// claimPollerIfFree makes workerID the getUpdates poller when none is set.
func (s *Server) claimPollerIfFree(ctx context.Context, workerID string) {
	cur, _, err := s.set.Get(ctx, settings.PollerWorkerID)
	if err != nil || cur != "" {
		return
	}
	if err := s.set.Set(ctx, settings.PollerWorkerID, workerID); err != nil {
		s.log.Warn("assign poller", "err", err)
		return
	}
	s.log.Info("poller assigned", "worker_id", workerID)
}

// pollerLoop hands getUpdates to another connected worker once the current
// poller has been gone for pollerGrace. Telegram allows only one consumer.
func (s *Server) pollerLoop(ctx context.Context) {
	t := time.NewTicker(pollerCheck)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.checkPoller(ctx)
		}
	}
}

func (s *Server) checkPoller(ctx context.Context) {
	cur, _, err := s.set.Get(ctx, settings.PollerWorkerID)
	if err != nil {
		return
	}
	if _, ok := s.conn(cur); ok {
		s.mu.Lock()
		s.pollerLostAt = time.Time{}
		s.mu.Unlock()
		return
	}
	now := s.now()
	s.mu.Lock()
	if s.pollerLostAt.IsZero() {
		s.pollerLostAt = now
	}
	lost := now.Sub(s.pollerLostAt)
	s.mu.Unlock()
	connected := s.ConnectedWorkers()
	if (cur != "" && lost < pollerGrace) || len(connected) == 0 {
		return
	}
	next := connected[0]
	if err := s.set.Set(ctx, settings.PollerWorkerID, next); err != nil {
		s.log.Warn("reassign poller", "err", err)
		return
	}
	s.mu.Lock()
	s.pollerLostAt = time.Time{}
	s.mu.Unlock()
	s.log.Info("poller reassigned", "from", cur, "to", next)
	if err := s.SendConfig(ctx, next); err != nil {
		s.log.Warn("send poller config", "err", err)
	}
}

// purgeLoop forgets processed update ids older than processedUpdatesTTL.
func (s *Server) purgeLoop(ctx context.Context) {
	t := time.NewTicker(purgeInterval)
	defer t.Stop()
	for {
		if err := s.purgeUpdates(ctx); err != nil && ctx.Err() == nil {
			s.log.Warn("purge bot updates", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (s *Server) purgeUpdates(ctx context.Context) error {
	_, err := s.st.DB().ExecContext(ctx,
		`DELETE FROM bot_updates WHERE processed_at < ?`, s.now().Add(-processedUpdatesTTL).UnixMilli())
	return err
}
