// Package runner executes tasks received from the core via the link stream.
// See docs/04-core-worker-protocol.md and docs/02-architecture.md (worker components).
package runner

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"google.golang.org/protobuf/proto"

	linkv1 "github.com/tuskar2321/quoker/gen/quoker/link/v1"
	workerlink "github.com/tuskar2321/quoker/internal/worker/link"
)

// Handlers holds the per-kind task implementations.
type Handlers struct {
	Resolve   ResolveHandler
	Download  DownloadHandler
	SendVideo SendVideoHandler
	BotCall   BotCallHandler
}

// ResolveHandler executes a resolve task and returns the result.
type ResolveHandler interface {
	Resolve(ctx context.Context, task *linkv1.ResolveTask, cfg *linkv1.Config, prog Progress) (*linkv1.ResolveResult, error)
}

// DownloadHandler executes a download task.
type DownloadHandler interface {
	Download(ctx context.Context, task *linkv1.DownloadTask, cfg *linkv1.Config, prog Progress) (*linkv1.DownloadResult, error)
}

// SendVideoHandler executes a send_video task.
type SendVideoHandler interface {
	SendVideo(ctx context.Context, task *linkv1.SendVideoTask, cfg *linkv1.Config, prog Progress) (*linkv1.SendVideoResult, error)
}

// BotCallHandler executes a bot_call task.
type BotCallHandler interface {
	BotCall(ctx context.Context, task *linkv1.BotCallTask, cfg *linkv1.Config) (*linkv1.BotCallResult, error)
}

// Progress lets a handler report intermediate progress.
type Progress func(percent int32)

// ConfigSource provides the current core Config (updated from the stream).
type ConfigSource interface {
	CoreConfig() *linkv1.Config
}

// Runner implements link.TaskHandler. It routes tasks by kind and handles
// panics. Concurrency is enforced by the link client (maxParallel slots).
type Runner struct {
	handlers Handlers
	cfgSrc   ConfigSource
	log      *slog.Logger
	maxP     int
}

// New returns a Runner. maxParallel must be > 0 (docs/02-architecture.md: default 2).
func New(h Handlers, cfgSrc ConfigSource, maxParallel int, log *slog.Logger) *Runner {
	if maxParallel <= 0 {
		maxParallel = 2
	}
	if log == nil {
		log = slog.Default()
	}
	return &Runner{handlers: h, cfgSrc: cfgSrc, log: log, maxP: maxParallel}
}

// Handle implements link.TaskHandler. It executes the task synchronously in
// the caller's goroutine; the link client is responsible for concurrency.
func (r *Runner) Handle(ctx context.Context, task *linkv1.Task, conn workerlink.Conn) error {
	log := r.log.With("task_id", task.TaskId)
	ctx, cancel := context.WithTimeout(ctx, taskDeadline(task))
	defer cancel()

	cfg := r.cfgSrc.CoreConfig()
	if cfg == nil {
		return r.sendError(conn, task.TaskId, "internal", "no config received yet", true)
	}

	prog := func(pct int32) {
		if err := conn.SendProgress(task.TaskId, pct); err != nil {
			log.Warn("send progress", "err", err)
		}
	}

	result, taskErr := r.dispatch(ctx, task, cfg, prog, log)

	if taskErr != nil {
		log.Info("task failed", "code", taskErr.Code, "retryable", taskErr.Retryable)
		return conn.SendResult(&linkv1.TaskResult{
			TaskId: task.TaskId,
			Result: &linkv1.TaskResult_Error{Error: taskErr},
		})
	}

	res := &linkv1.TaskResult{TaskId: task.TaskId}
	switch v := result.(type) {
	case *linkv1.ResolveResult:
		res.Result = &linkv1.TaskResult_Resolve{Resolve: v}
	case *linkv1.DownloadResult:
		res.Result = &linkv1.TaskResult_Download{Download: v}
	case *linkv1.SendVideoResult:
		res.Result = &linkv1.TaskResult_SendVideo{SendVideo: v}
	case *linkv1.BotCallResult:
		res.Result = &linkv1.TaskResult_BotCall{BotCall: v}
	default:
		return r.sendError(conn, task.TaskId, "internal", fmt.Sprintf("unexpected result type %T", result), true)
	}
	log.Info("task done")
	return conn.SendResult(res)
}

func (r *Runner) dispatch(ctx context.Context, task *linkv1.Task, cfg *linkv1.Config, prog Progress, log *slog.Logger) (result proto.Message, taskErr *linkv1.TaskError) {
	defer func() {
		if v := recover(); v != nil {
			log.Error("task panic", "panic", v)
			taskErr = errInternal(fmt.Sprintf("panic: %v", v))
			result = nil
		}
	}()

	switch k := task.Kind.(type) {
	case *linkv1.Task_Resolve:
		if r.handlers.Resolve == nil {
			return nil, errInternal("resolve handler not configured")
		}
		res, err := r.handlers.Resolve.Resolve(ctx, k.Resolve, cfg, prog)
		if err != nil {
			return nil, toTaskError(err)
		}
		return res, nil

	case *linkv1.Task_Download:
		if r.handlers.Download == nil {
			return nil, errInternal("download handler not configured")
		}
		res, err := r.handlers.Download.Download(ctx, k.Download, cfg, prog)
		if err != nil {
			return nil, toTaskError(err)
		}
		return res, nil

	case *linkv1.Task_SendVideo:
		if r.handlers.SendVideo == nil {
			return nil, errInternal("send_video handler not configured")
		}
		res, err := r.handlers.SendVideo.SendVideo(ctx, k.SendVideo, cfg, prog)
		if err != nil {
			return nil, toTaskError(err)
		}
		return res, nil

	case *linkv1.Task_BotCall:
		if r.handlers.BotCall == nil {
			return nil, errInternal("bot_call handler not configured")
		}
		res, err := r.handlers.BotCall.BotCall(ctx, k.BotCall, cfg)
		if err != nil {
			return nil, toTaskError(err)
		}
		return res, nil

	default:
		return nil, errInternal(fmt.Sprintf("unknown task kind %T", task.Kind))
	}
}

func (r *Runner) sendError(conn workerlink.Conn, taskID, code, msg string, retryable bool) error {
	return conn.SendResult(&linkv1.TaskResult{
		TaskId: taskID,
		Result: &linkv1.TaskResult_Error{
			Error: &linkv1.TaskError{Code: code, Message: msg, Retryable: retryable},
		},
	})
}

// TaskError wraps a worker error code with retryability.
type TaskError struct {
	Code      string
	Message   string
	Retryable bool
	// Raw Bot API error, when the failure came from Telegram.
	TelegramCode        int32
	TelegramDescription string
	// Cause is the underlying error; it is logged but never sent to the core.
	Cause error
}

func (e *TaskError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *TaskError) Unwrap() error { return e.Cause }

// NewError creates a TaskError.
func NewError(code, message string, retryable bool, cause error) *TaskError {
	return &TaskError{Code: code, Message: message, Retryable: retryable, Cause: cause}
}

// ErrCancelled is returned when the task context was cancelled.
var ErrCancelled = NewError("cancelled", "task cancelled", false, nil)

// toTaskError converts an arbitrary error to a *linkv1.TaskError.
func toTaskError(err error) *linkv1.TaskError {
	if err == nil {
		return nil
	}
	var te *TaskError
	if errors.As(err, &te) {
		return &linkv1.TaskError{
			Code:                te.Code,
			Message:             te.Message,
			Retryable:           te.Retryable,
			TelegramErrorCode:   te.TelegramCode,
			TelegramDescription: te.TelegramDescription,
		}
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return &linkv1.TaskError{Code: "cancelled", Retryable: false}
	}
	return errInternal(err.Error())
}

func errInternal(msg string) *linkv1.TaskError {
	return &linkv1.TaskError{Code: "internal", Message: msg, Retryable: true}
}

const (
	// longTaskDeadline caps downloads and sends: their leases are renewed by
	// progress, so without a cap a hung upload would hold a slot forever.
	longTaskDeadline = 30 * time.Minute
	// leaseMargin leaves time to report before the core's lease runs out.
	leaseMargin = 5 * time.Second
)

// taskDeadline bounds a task: short tasks must finish within their lease,
// long ones within longTaskDeadline.
func taskDeadline(task *linkv1.Task) time.Duration {
	switch task.Kind.(type) {
	case *linkv1.Task_Download, *linkv1.Task_SendVideo:
		return longTaskDeadline
	}
	lease := time.Duration(task.LeaseMs) * time.Millisecond
	if lease <= leaseMargin {
		return time.Minute
	}
	return lease - leaseMargin
}
