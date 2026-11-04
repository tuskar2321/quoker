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
	// backoffMin is the minimum reconnect delay.
	backoffMin = time.Second
	// backoffMax is the maximum reconnect delay.
	backoffMax = 60 * time.Second
	// jitter is the fraction of the delay to randomize (±20%).
	jitter = 0.2
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

// Client manages the persistent bidirectional stream to the core.
type Client struct {
	cfg Config
	log *slog.Logger
	now func() time.Time

	mu     sync.Mutex
	stream linkv1.LinkService_ConnectClient
	sc     *streamConn    // writer of the live stream, nil when disconnected
	cfgV   *linkv1.Config // last received Config from core
}

// Connected reports whether a stream to the core is up.
func (c *Client) Connected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sc != nil
}

// SendBotUpdate forwards a Telegram update to the core.
func (c *Client) SendBotUpdate(updateID int64, updateJSON []byte) error {
	c.mu.Lock()
	sc := c.sc
	c.mu.Unlock()
	if sc == nil {
		return ErrNotConnected
	}
	return sc.send(&linkv1.WorkerMessage{Msg: &linkv1.WorkerMessage_BotUpdate{
		BotUpdate: &linkv1.BotUpdate{UpdateId: updateID, UpdateJson: string(updateJSON)},
	}})
}

// New creates a Client. Call Run to start connecting.
func New(cfg Config, log *slog.Logger, now func() time.Time) *Client {
	if now == nil {
		now = time.Now
	}
	if cfg.MaxParallel <= 0 {
		cfg.MaxParallel = 2
	}
	return &Client{cfg: cfg, log: log, now: now}
}

// CoreConfig returns the last Config received from the core. May be nil if
// the client has not yet connected.
func (c *Client) CoreConfig() *linkv1.Config {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cfgV
}

// Run connects to the core and handles messages until ctx is cancelled. It
// reconnects with exponential backoff on disconnection.
func (c *Client) Run(ctx context.Context) {
	attempts := 0
	for {
		if err := ctx.Err(); err != nil {
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
		if err := c.connect(ctx); err != nil {
			if ctx.Err() != nil {
				return
			}
			c.log.Warn("connection error", "err", err, "attempt", attempts)
		} else {
			attempts = 0
		}
	}
}

func (c *Client) connect(ctx context.Context) error {
	var dialOpts []grpc.DialOption
	if c.cfg.TLSConfig != nil {
		dialOpts = append(dialOpts, grpc.WithTransportCredentials(credentials.NewTLS(c.cfg.TLSConfig)))
	} else {
		// Development mode: no TLS. The server must accept h2c or plaintext.
		dialOpts = append(dialOpts, grpc.WithInsecure()) //nolint:staticcheck // dev only
	}

	cc, err := grpc.NewClient(c.cfg.CoreAddr, dialOpts...)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer func() { _ = cc.Close() }()

	// Inject cert headers for the server's cert verification (dev/test mode).
	// In production Caddy injects these from the mTLS handshake.
	streamCtx := metadata.AppendToOutgoingContext(ctx,
		"x-client-cert-cn", c.cfg.WorkerID,
		"x-client-cert-fingerprint", c.cfg.WorkerID, // placeholder; Caddy overwrites
	)

	stub := linkv1.NewLinkServiceClient(cc)
	stream, err := stub.Connect(streamCtx)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}

	// Send Hello.
	if err := stream.Send(&linkv1.WorkerMessage{
		Msg: &linkv1.WorkerMessage_Hello{
			Hello: &linkv1.Hello{
				WorkerId:     c.cfg.WorkerID,
				Version:      c.cfg.Version,
				YtdlpVersion: c.cfg.YtdlpVersion,
				MaxParallel:  c.cfg.MaxParallel,
			},
		},
	}); err != nil {
		return fmt.Errorf("send hello: %w", err)
	}
	c.log.Info("connected to core", "addr", c.cfg.CoreAddr)

	c.mu.Lock()
	c.stream = stream
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.stream = nil
		c.mu.Unlock()
	}()

	return c.runStream(ctx, stream)
}

// streamConn implements Conn for a live stream.
type streamConn struct {
	mu     sync.Mutex
	stream linkv1.LinkService_ConnectClient
}

// send serialises writes: gRPC streams forbid concurrent Send.
func (sc *streamConn) send(m *linkv1.WorkerMessage) error {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	return sc.stream.Send(m)
}

func (sc *streamConn) SendResult(r *linkv1.TaskResult) error {
	return sc.send(&linkv1.WorkerMessage{Msg: &linkv1.WorkerMessage_TaskResult{TaskResult: r}})
}

func (sc *streamConn) SendProgress(taskID string, percent int32) error {
	return sc.send(&linkv1.WorkerMessage{
		Msg: &linkv1.WorkerMessage_TaskProgress{
			TaskProgress: &linkv1.TaskProgress{TaskId: taskID, Percent: percent},
		},
	})
}

func (c *Client) runStream(ctx context.Context, stream linkv1.LinkService_ConnectClient) error {
	sc := &streamConn{stream: stream}
	c.mu.Lock()
	c.sc = sc
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.sc = nil
		c.mu.Unlock()
	}()

	taskCtx, cancelTasks := context.WithCancel(ctx)
	defer cancelTasks()

	var wg sync.WaitGroup
	activeTasks := make(map[string]context.CancelFunc)
	var taskMu sync.Mutex

	hbTick := time.NewTicker(heartbeatInterval)
	defer hbTick.Stop()

	recvCh := make(chan recvResult, 4)
	go func() {
		for {
			msg, err := stream.Recv()
			recvCh <- recvResult{msg: msg, err: err}
			if err != nil {
				return
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			cancelTasks()
			wg.Wait()
			return nil

		case <-hbTick.C:
			taskMu.Lock()
			runningN := len(activeTasks)
			taskMu.Unlock()
			if err := sc.send(&linkv1.WorkerMessage{
				Msg: &linkv1.WorkerMessage_Heartbeat{
					Heartbeat: &linkv1.Heartbeat{
						UnixMs:       c.now().UnixMilli(),
						RunningTasks: int32(runningN), //nolint:gosec // task count fits int32
					},
				},
			}); err != nil {
				return fmt.Errorf("send heartbeat: %w", err)
			}

		case r := <-recvCh:
			if r.err != nil {
				cancelTasks()
				wg.Wait()
				return fmt.Errorf("recv: %w", r.err)
			}
			msg := r.msg
			switch v := msg.Msg.(type) {
			case *linkv1.CoreMessage_Config:
				c.mu.Lock()
				c.cfgV = v.Config
				c.mu.Unlock()
				c.log.Info("config received", "poll_updates", v.Config.PollUpdates)

			case *linkv1.CoreMessage_Task:
				task := v.Task
				tCtx, tCancel := context.WithCancel(taskCtx)
				taskMu.Lock()
				activeTasks[task.TaskId] = tCancel
				taskMu.Unlock()
				wg.Add(1)
				go func() {
					defer wg.Done()
					defer func() {
						tCancel()
						taskMu.Lock()
						delete(activeTasks, task.TaskId)
						taskMu.Unlock()
					}()
					if err := c.cfg.Handler.Handle(tCtx, task, sc); err != nil {
						c.log.Warn("task error", "task", task.TaskId, "err", err)
					}
				}()

			case *linkv1.CoreMessage_CancelTask:
				taskMu.Lock()
				if cancel, ok := activeTasks[v.CancelTask.TaskId]; ok {
					cancel()
				}
				taskMu.Unlock()

			case *linkv1.CoreMessage_Heartbeat:
				// no-op; we just reset our timeout via receiving any message

			case *linkv1.CoreMessage_BotUpdateAck:
				if c.cfg.OnBotUpdateAck != nil {
					c.cfg.OnBotUpdateAck(v.BotUpdateAck.UpdateId)
				}

			case *linkv1.CoreMessage_Shutdown:
				c.log.Info("shutdown requested", "reason", v.Shutdown.Reason)
				cancelTasks()
				wg.Wait()
				// If revoked, don't reconnect.
				if v.Shutdown.Reason == "revoked" {
					return errRevoked
				}
				return nil
			}
		}
	}
}

type recvResult struct {
	msg *linkv1.CoreMessage
	err error
}

// errRevoked signals the worker should not reconnect.
var errRevoked = fmt.Errorf("worker revoked")

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
