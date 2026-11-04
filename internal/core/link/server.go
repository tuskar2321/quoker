// Package link implements the gRPC server that worker processes connect to.
// See docs/04-core-worker-protocol.md and proto/quoker/link/v1/link.proto.
package link

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

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
	// headerFingerprint is set by Caddy after mTLS verification.
	headerFingerprint = "X-Client-Cert-Fingerprint"
	// headerCertCN is set by Caddy to the certificate's Common Name.
	headerCertCN = "X-Client-Cert-CN"
)

// Deps are the dependencies of the link server.
type Deps struct {
	Store    *store.Store
	Queue    *queue.Store
	Settings *settings.Store
	Log      *slog.Logger
	Now      func() time.Time
}

// Server is the gRPC LinkService implementation.
type Server struct {
	linkv1.UnimplementedLinkServiceServer
	st  *store.Store
	q   *queue.Store
	set *settings.Store
	log *slog.Logger
	now func() time.Time

	mu      sync.Mutex
	workers map[string]*workerConn // keyed by worker_id
}

// New returns a Server. It also starts background goroutines for lease
// recovery and config broadcasts; use a context to stop them.
func New(ctx context.Context, d Deps) *Server {
	srv := &Server{
		st:      d.Store,
		q:       d.Queue,
		set:     d.Settings,
		log:     d.Log,
		now:     d.Now,
		workers: make(map[string]*workerConn),
	}
	go srv.leaseRecoveryLoop(ctx)
	return srv
}

// workerConn tracks an active worker stream.
type workerConn struct {
	id          string
	stream      linkv1.LinkService_ConnectServer
	lastHB      time.Time
	tasks       map[string]struct{} // task IDs currently leased to this worker
	taskCh      chan *linkv1.Task   // tasks to send; closed on disconnect
	cancelCh    chan string         // task IDs to cancel; closed on disconnect
	maxParallel int
	done        chan struct{}
}

// Connect implements LinkServiceServer.
func (s *Server) Connect(stream linkv1.LinkService_ConnectServer) error {
	fingerprint, cn, err := s.certInfo(stream.Context())
	if err != nil {
		return status.Error(codes.Unauthenticated, err.Error())
	}

	// Verify worker is known and allowed.
	workerID, err := s.verifyWorker(stream.Context(), fingerprint, cn)
	if err != nil {
		return status.Error(codes.PermissionDenied, err.Error())
	}
	log := s.log.With("worker_id", workerID)

	// Receive Hello.
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
		stream:      stream,
		lastHB:      s.now(),
		tasks:       make(map[string]struct{}),
		taskCh:      make(chan *linkv1.Task, 8),
		cancelCh:    make(chan string, 8),
		maxParallel: maxParallel,
		done:        make(chan struct{}),
	}
	s.registerWorker(conn)
	defer s.unregisterWorker(conn)

	// Update worker metadata in DB.
	s.updateWorkerSeen(stream.Context(), workerID, hello)

	// Send initial Config.
	cfg, err := s.loadConfig(stream.Context(), workerID)
	if err != nil {
		return status.Errorf(codes.Internal, "load config: %v", err)
	}
	if err := stream.Send(&linkv1.CoreMessage{Msg: &linkv1.CoreMessage_Config{Config: cfg}}); err != nil {
		return err
	}
	log.Info("worker connected", "max_parallel", maxParallel, "version", hello.Version)

	ctx, cancel := context.WithCancel(stream.Context())
	defer cancel()

	// Sender goroutine: drains taskCh and cancelCh.
	sendErr := make(chan error, 1)
	go func() {
		sendErr <- s.senderLoop(ctx, conn, stream)
	}()

	// Task dispatcher goroutine: picks tasks from queue and sends them.
	go s.dispatchLoop(ctx, conn)

	// Heartbeat ticker.
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
			if err := stream.Send(&linkv1.CoreMessage{
				Msg: &linkv1.CoreMessage_Heartbeat{
					Heartbeat: &linkv1.Heartbeat{UnixMs: s.now().UnixMilli()},
				},
			}); err != nil {
				return err
			}
		case <-hbDeadline.C:
			log.Warn("heartbeat timeout, closing stream")
			return status.Error(codes.DeadlineExceeded, "heartbeat timeout")
		default:
			// Receive next message.
			msg, err := stream.Recv()
			if err != nil {
				return err
			}
			hbDeadline.Reset(heartbeatTimeout)
			if err := s.handleWorkerMsg(stream.Context(), conn, msg, log); err != nil {
				return err
			}
		}
	}
}

func (s *Server) handleWorkerMsg(ctx context.Context, conn *workerConn, msg *linkv1.WorkerMessage, log *slog.Logger) error {
	switch v := msg.Msg.(type) {
	case *linkv1.WorkerMessage_Heartbeat:
		s.mu.Lock()
		conn.lastHB = s.now()
		s.mu.Unlock()

	case *linkv1.WorkerMessage_TaskProgress:
		if err := s.q.SetProgress(ctx, v.TaskProgress.TaskId, conn.id, int(v.TaskProgress.Percent)); err != nil {
			log.Warn("set progress failed", "task", v.TaskProgress.TaskId, "err", err)
		}

	case *linkv1.WorkerMessage_TaskResult:
		if err := s.handleTaskResult(ctx, conn, v.TaskResult, log); err != nil {
			log.Warn("handle task result failed", "task", v.TaskResult.TaskId, "err", err)
		}

	case *linkv1.WorkerMessage_BotUpdate:
		if err := s.handleBotUpdate(ctx, conn, v.BotUpdate, log); err != nil {
			log.Warn("handle bot update failed", "update_id", v.BotUpdate.UpdateId, "err", err)
		}
	}
	return nil
}

func (s *Server) handleTaskResult(ctx context.Context, conn *workerConn, r *linkv1.TaskResult, log *slog.Logger) error {
	s.mu.Lock()
	delete(conn.tasks, r.TaskId)
	s.mu.Unlock()

	var p queue.CompleteParams
	p.TaskID = r.TaskId
	p.WorkerID = conn.id

	if e := r.GetError(); e != nil {
		p.ErrCode = e.Code
		p.ErrMsg = e.Message
		p.Retryable = e.Retryable
	}
	// TODO: when videos/sends packages exist, decode and apply the result.
	if err := s.q.Complete(ctx, p); err != nil {
		log.Warn("complete task", "task", r.TaskId, "err", err)
	}
	log.Info("task result", "task", r.TaskId, "error_code", p.ErrCode)
	return nil
}

func (s *Server) handleBotUpdate(ctx context.Context, conn *workerConn, u *linkv1.BotUpdate, log *slog.Logger) error {
	// Store update_id for idempotency.
	_, err := s.st.DB().ExecContext(ctx,
		`INSERT OR IGNORE INTO bot_updates (update_id, processed_at) VALUES (?, ?)`,
		u.UpdateId, s.now().UnixMilli())
	if err != nil {
		return fmt.Errorf("record bot update: %w", err)
	}
	// TODO: dispatch to bot package for processing.
	log.Info("bot update received", "update_id", u.UpdateId)

	// Ack so the worker advances its offset.
	return conn.stream.Send(&linkv1.CoreMessage{
		Msg: &linkv1.CoreMessage_BotUpdateAck{
			BotUpdateAck: &linkv1.BotUpdateAck{UpdateId: u.UpdateId},
		},
	})
}

// senderLoop drains taskCh and cancelCh and writes to the stream.
func (s *Server) senderLoop(ctx context.Context, conn *workerConn, stream linkv1.LinkService_ConnectServer) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case t, ok := <-conn.taskCh:
			if !ok {
				return nil
			}
			if err := stream.Send(&linkv1.CoreMessage{Msg: &linkv1.CoreMessage_Task{Task: t}}); err != nil {
				return err
			}
		case taskID, ok := <-conn.cancelCh:
			if !ok {
				return nil
			}
			if err := stream.Send(&linkv1.CoreMessage{
				Msg: &linkv1.CoreMessage_CancelTask{
					CancelTask: &linkv1.CancelTask{TaskId: taskID},
				},
			}); err != nil {
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
			s.mu.Lock()
			running := len(conn.tasks)
			maxP := conn.maxParallel
			s.mu.Unlock()
			for running < maxP {
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
					_ = s.q.Complete(ctx, queue.CompleteParams{
						TaskID: task.ID, WorkerID: conn.id,
						ErrCode: "internal", ErrMsg: err.Error(), Retryable: true,
					})
					break
				}
				s.mu.Lock()
				conn.tasks[task.ID] = struct{}{}
				running = len(conn.tasks)
				s.mu.Unlock()
				select {
				case conn.taskCh <- protoTask:
				case <-ctx.Done():
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
		MaxSendBytes:     50 * 1024 * 1024,
		MaxDownloadBytes: 100 * 1024 * 1024,
		BotApiBase:       "https://api.telegram.org",
	}, nil
}

func (s *Server) registerWorker(conn *workerConn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.workers[conn.id] = conn
}

func (s *Server) unregisterWorker(conn *workerConn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.workers, conn.id)
	close(conn.done)
}

// SendConfig broadcasts a fresh Config to a specific worker (called when
// settings change).
func (s *Server) SendConfig(ctx context.Context, workerID string) error {
	s.mu.Lock()
	conn, ok := s.workers[workerID]
	s.mu.Unlock()
	if !ok {
		return nil // worker not connected; it will get the config on next connect
	}
	cfg, err := s.loadConfig(ctx, workerID)
	if err != nil {
		return err
	}
	return conn.stream.Send(&linkv1.CoreMessage{Msg: &linkv1.CoreMessage_Config{Config: cfg}})
}

// CancelTask sends a CancelTask message to the worker holding the task.
func (s *Server) CancelTask(workerID, taskID string) {
	s.mu.Lock()
	conn, ok := s.workers[workerID]
	s.mu.Unlock()
	if !ok {
		return
	}
	select {
	case conn.cancelCh <- taskID:
	default:
	}
}

// Shutdown sends a Shutdown message to a worker and removes it.
func (s *Server) Shutdown(_ context.Context, workerID, reason string) error {
	s.mu.Lock()
	conn, ok := s.workers[workerID]
	s.mu.Unlock()
	if !ok {
		return nil
	}
	return conn.stream.Send(&linkv1.CoreMessage{
		Msg: &linkv1.CoreMessage_Shutdown{Shutdown: &linkv1.Shutdown{Reason: reason}},
	})
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
			} else if rec+failed > 0 {
				s.log.Info("lease recovery", "recovered", rec, "failed", failed)
			}
		}
	}
}

// verifyWorker looks up the worker in the DB by fingerprint and checks status.
func (s *Server) verifyWorker(ctx context.Context, fingerprint, _ string) (string, error) {
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
