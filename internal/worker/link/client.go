// Package link implements the gRPC client for the worker side of the
// core ↔ worker protocol. See docs/04-core-worker-protocol.md.
package link

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"

	linkv1 "github.com/tuskar2321/quoker/gen/quoker/link/v1"
)

const (
	// heartbeatInterval is how often the worker sends heartbeats.
	heartbeatInterval = 15 * time.Second
	// silenceTimeout: no message from the core for this long means the
	// connection is dead even if writes still succeed (docs/04).
	silenceTimeout = 45 * time.Second
	silenceCheck   = 5 * time.Second
	// backoffMin is the minimum reconnect delay.
	backoffMin = time.Second
	// backoffMax is the maximum reconnect delay.
	backoffMax = 60 * time.Second
	// jitter is the fraction of the delay to randomize (±20%).
	jitter = 0.2
	// maxOutbox bounds results waiting for a reconnect.
	maxOutbox = 100
)

// TaskHandler is called for each task dispatched by the core.
type TaskHandler interface {
	// Handle executes the task. It must return when ctx is cancelled.
	Handle(ctx context.Context, task *linkv1.Task, conn Conn) error
}

// Conn lets a TaskHandler send results and progress to the core.
type Conn interface {
	SendResult(r *linkv1.TaskResult) error
	SendProgress(taskID string, percent int32) error
}

// Config is the configuration for the worker link client.
type Config struct {
	WorkerID     string
	Version      string
	YtdlpVersion string
	MaxParallel  int32
	// CoreAddr is the gRPC endpoint (host:port).
	CoreAddr string
	// TLSConfig is the mTLS config (client cert + CA for the core).
	TLSConfig *tls.Config
	// Handler processes incoming tasks.
	Handler TaskHandler
	// OnBotUpdateAck, when set, receives update ids the core has processed.
	OnBotUpdateAck func(updateID int64)
}

// ErrNotConnected is returned when there is no live stream to the core.
var ErrNotConnected = errors.New("not connected to core")

// errRevoked signals the worker should not reconnect.
var errRevoked = errors.New("worker revoked")

// Client manages the persistent bidirectional stream to the core. Tasks run
// at the client level, not per stream: a dropped connection does not stop
// them, and results that could not be sent wait in an outbox for the next
// stream (docs/11: a sent video must not be posted again).
type Client struct {
	cfg Config
	log *slog.Logger
	now func() time.Time

	mu     sync.Mutex
	sc     *streamConn    // writer of the live stream, nil when disconnected
	cfgV   *linkv1.Config // last received Config from core
	tasks  map[string]context.CancelFunc
	outbox []*linkv1.TaskResult
	wg     sync.WaitGroup
}

// New creates a Client. Call Run to start connecting.
func New(cfg Config, log *slog.Logger, now func() time.Time) *Client {
	if now == nil {
		now = time.Now
	}
	if cfg.MaxParallel <= 0 {
		cfg.MaxParallel = 2
	}
	return &Client{cfg: cfg, log: log, now: now, tasks: map[string]context.CancelFunc{}}
}

// CoreConfig returns the last Config received from the core. May be nil if
// the client has not yet connected.
func (c *Client) CoreConfig() *linkv1.Config {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cfgV
}

// Connected reports whether a stream to the core is up.
func (c *Client) Connected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sc != nil
}

// SendBotUpdate forwards a Telegram update to the core.
func (c *Client) SendBotUpdate(updateID int64, updateJSON []byte) error {
	sc := c.stream()
	if sc == nil {
		return ErrNotConnected
	}
	return sc.send(&linkv1.WorkerMessage{Msg: &linkv1.WorkerMessage_BotUpdate{
		BotUpdate: &linkv1.BotUpdate{UpdateId: updateID, UpdateJson: string(updateJSON)},
	}})
}

// SendResult implements Conn: the result goes out now, or after the next
// reconnect if the stream is down.
func (c *Client) SendResult(r *linkv1.TaskResult) error {
	if sc := c.stream(); sc != nil {
		if err := sc.sendResult(r); err == nil {
			return nil
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.outbox) >= maxOutbox {
		c.log.Error("outbox full, dropping oldest result", "task_id", c.outbox[0].TaskId)
		c.outbox = c.outbox[1:]
	}
	c.outbox = append(c.outbox, r)
	c.log.Info("result kept until reconnect", "task_id", r.TaskId)
	return nil
}

// SendProgress implements Conn. Progress only renews the lease, so it is
// dropped while disconnected.
func (c *Client) SendProgress(taskID string, percent int32) error {
	sc := c.stream()
	if sc == nil {
		return ErrNotConnected
	}
	return sc.send(&linkv1.WorkerMessage{Msg: &linkv1.WorkerMessage_TaskProgress{
		TaskProgress: &linkv1.TaskProgress{TaskId: taskID, Percent: percent},
	}})
}

func (c *Client) stream() *streamConn {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sc
}

// flushOutbox re-sends results kept during a disconnect.
func (c *Client) flushOutbox(sc *streamConn) {
	c.mu.Lock()
	pending := c.outbox
	c.outbox = nil
	c.mu.Unlock()
	for i, r := range pending {
		if err := sc.sendResult(r); err != nil {
			c.mu.Lock()
			c.outbox = append(pending[i:], c.outbox...)
			c.mu.Unlock()
			return
		}
		c.log.Info("kept result delivered", "task_id", r.TaskId)
	}
}

// Run connects to the core and handles messages until ctx is cancelled. It
// reconnects with exponential backoff, and stops for good once revoked.
// Running tasks are cancelled when ctx ends.
func (c *Client) Run(ctx context.Context) {
	defer c.wg.Wait()
	attempts := 0
	for {
		if ctx.Err() != nil {
			return
		}
		if attempts > 0 {
			delay := backoffDelay(attempts)
			c.log.Info("reconnecting", "in", delay)
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return
			}
		}
		attempts++
		healthy, err := c.connect(ctx)
		if healthy {
			attempts = 1 // the next drop retries after a second, not after the old backoff
		}
		switch {
		case ctx.Err() != nil:
			return
		case errors.Is(err, errRevoked):
			c.log.Warn("worker revoked by the core, not reconnecting")
			return
		case err != nil:
			c.log.Warn("connection error", "err", err, "attempt", attempts)
		}
	}
}

// connect runs one stream. healthy reports whether the core accepted us and
// sent a Config, which resets the backoff.
func (c *Client) connect(ctx context.Context) (healthy bool, err error) {
	var dialOpts []grpc.DialOption
	if c.cfg.TLSConfig != nil {
		dialOpts = append(dialOpts, grpc.WithTransportCredentials(credentials.NewTLS(c.cfg.TLSConfig)))
	} else {
		// Development mode: no TLS. The server must accept h2c or plaintext.
		dialOpts = append(dialOpts, grpc.WithInsecure()) //nolint:staticcheck // dev only
	}
	cc, err := grpc.NewClient(c.cfg.CoreAddr, dialOpts...)
	if err != nil {
		return false, fmt.Errorf("dial: %w", err)
	}
	defer func() { _ = cc.Close() }()

	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	// Inject cert headers for the server's cert verification (dev/test mode).
	// In production Caddy injects these from the mTLS handshake.
	streamCtx = metadata.AppendToOutgoingContext(streamCtx,
		"x-client-cert-cn", c.cfg.WorkerID,
		"x-client-cert-fingerprint", c.cfg.WorkerID, // placeholder; Caddy overwrites
	)
	stream, err := linkv1.NewLinkServiceClient(cc).Connect(streamCtx)
	if err != nil {
		return false, fmt.Errorf("connect: %w", err)
	}
	if err := stream.Send(&linkv1.WorkerMessage{Msg: &linkv1.WorkerMessage_Hello{Hello: &linkv1.Hello{
		WorkerId: c.cfg.WorkerID, Version: c.cfg.Version, YtdlpVersion: c.cfg.YtdlpVersion, MaxParallel: c.cfg.MaxParallel,
	}}}); err != nil {
		return false, fmt.Errorf("send hello: %w", err)
	}
	c.log.Info("connected to core", "addr", c.cfg.CoreAddr)
	return c.runStream(ctx, streamCtx, stream)
}

// streamConn serialises writes: gRPC streams forbid concurrent Send.
type streamConn struct {
	mu     sync.Mutex
	stream linkv1.LinkService_ConnectClient
}

func (sc *streamConn) send(m *linkv1.WorkerMessage) error {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	return sc.stream.Send(m)
}

func (sc *streamConn) sendResult(r *linkv1.TaskResult) error {
	return sc.send(&linkv1.WorkerMessage{Msg: &linkv1.WorkerMessage_TaskResult{TaskResult: r}})
}

type recvResult struct {
	msg *linkv1.CoreMessage
	err error
}

func (c *Client) runStream(ctx, streamCtx context.Context, stream linkv1.LinkService_ConnectClient) (healthy bool, err error) {
	sc := &streamConn{stream: stream}
	defer func() {
		c.mu.Lock()
		if c.sc == sc {
			c.sc = nil
		}
		c.mu.Unlock()
	}()

	recvCh := make(chan recvResult, 4)
	go func() {
		for {
			msg, err := stream.Recv()
			select {
			case recvCh <- recvResult{msg: msg, err: err}:
			case <-streamCtx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()

	hbTick := time.NewTicker(heartbeatInterval)
	defer hbTick.Stop()
	watchdog := time.NewTicker(silenceCheck)
	defer watchdog.Stop()
	lastHeard := c.now()

	for {
		select {
		case <-ctx.Done():
			return healthy, nil

		case <-watchdog.C:
			if c.now().Sub(lastHeard) > silenceTimeout {
				return healthy, fmt.Errorf("no message from core for %s", silenceTimeout)
			}

		case <-hbTick.C:
			if err := sc.send(&linkv1.WorkerMessage{Msg: &linkv1.WorkerMessage_Heartbeat{Heartbeat: &linkv1.Heartbeat{
				UnixMs: c.now().UnixMilli(), RunningTasks: int32(c.running()), //nolint:gosec // small count
			}}}); err != nil {
				return healthy, fmt.Errorf("send heartbeat: %w", err)
			}

		case r := <-recvCh:
			if r.err != nil {
				return healthy, fmt.Errorf("recv: %w", r.err)
			}
			lastHeard = c.now()
			switch v := r.msg.Msg.(type) {
			case *linkv1.CoreMessage_Config:
				c.mu.Lock()
				c.cfgV = v.Config
				first := c.sc != sc
				c.sc = sc
				c.mu.Unlock()
				c.log.Info("config received", "poll_updates", v.Config.PollUpdates)
				if first {
					healthy = true
					c.flushOutbox(sc)
				}

			case *linkv1.CoreMessage_Task:
				c.startTask(ctx, v.Task, sc)

			case *linkv1.CoreMessage_CancelTask:
				c.mu.Lock()
				if cancel, ok := c.tasks[v.CancelTask.TaskId]; ok {
					cancel()
				}
				c.mu.Unlock()

			case *linkv1.CoreMessage_BotUpdateAck:
				if c.cfg.OnBotUpdateAck != nil {
					c.cfg.OnBotUpdateAck(v.BotUpdateAck.UpdateId)
				}

			case *linkv1.CoreMessage_Shutdown:
				c.log.Info("shutdown requested", "reason", v.Shutdown.Reason)
				if v.Shutdown.Reason == "revoked" {
					c.cancelAll()
					return healthy, errRevoked
				}
				// The core restarts; tasks keep running and report after
				// the reconnect.
				return healthy, nil
			}
		}
	}
}

// startTask runs a dispatched task unless it is already running here (the
// core requeued it while we were disconnected) or all slots are busy.
func (c *Client) startTask(ctx context.Context, task *linkv1.Task, sc *streamConn) {
	c.mu.Lock()
	if _, running := c.tasks[task.TaskId]; running {
		c.mu.Unlock()
		c.log.Info("task already running here", "task_id", task.TaskId)
		return
	}
	if len(c.tasks) >= int(c.cfg.MaxParallel) {
		c.mu.Unlock()
		c.log.Warn("all slots busy, task returned", "task_id", task.TaskId)
		_ = sc.sendResult(&linkv1.TaskResult{TaskId: task.TaskId, Result: &linkv1.TaskResult_Error{
			Error: &linkv1.TaskError{Code: "internal", Message: "worker busy", Retryable: true},
		}})
		return
	}
	tCtx, cancel := context.WithCancel(ctx)
	c.tasks[task.TaskId] = cancel
	c.wg.Add(1)
	c.mu.Unlock()
	go func() {
		defer c.wg.Done()
		defer func() {
			cancel()
			c.mu.Lock()
			delete(c.tasks, task.TaskId)
			c.mu.Unlock()
		}()
		if err := c.cfg.Handler.Handle(tCtx, task, c); err != nil {
			c.log.Warn("task error", "task_id", task.TaskId, "err", err)
		}
	}()
}

func (c *Client) running() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.tasks)
}

func (c *Client) cancelAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, cancel := range c.tasks {
		cancel()
	}
}

// backoffDelay computes an exponential delay with ±20% jitter.
func backoffDelay(attempts int) time.Duration {
	shift := attempts - 1
	if shift > 6 {
		shift = 6
	}
	base := backoffMin * (1 << shift)
	if base > backoffMax {
		base = backoffMax
	}
	j := float64(base) * jitter * (rand.Float64()*2 - 1) //nolint:gosec // G404: jitter, not crypto
	return base + time.Duration(j)
}
