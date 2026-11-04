// Package tasks implements the worker task handlers on top of yt-dlp, the
// Bot API client, the core upload client and the temporary file cache.
// See docs/04-core-worker-protocol.md and docs/07-downloading.md.
package tasks

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/tuskar2321/quoker/internal/shared/errs"
	"github.com/tuskar2321/quoker/internal/worker/runner"
	"github.com/tuskar2321/quoker/internal/worker/telegram"
	"github.com/tuskar2321/quoker/internal/worker/tmpcache"
	"github.com/tuskar2321/quoker/internal/worker/upload"
	"github.com/tuskar2321/quoker/internal/worker/ytdlp"
)

const (
	// maxDurationS rejects long videos (docs/11-nfr-security.md).
	maxDurationS = 10 * 60
	// keepAliveEvery renews leases of long tasks; well under lease/3.
	keepAliveEvery = 60 * time.Second
	botCallTimeout = 25 * time.Second
)

// Deps are the collaborators of the handlers.
type Deps struct {
	YtDlp     *ytdlp.Runner
	Telegram  *telegram.Client
	Upload    *upload.Client
	Cache     *tmpcache.Cache
	Web       *http.Client // fetches thumbnails from the platforms
	UserAgent string
	Log       *slog.Logger
}

// Handlers implements every runner handler interface.
type Handlers struct {
	d         Deps
	keepAlive time.Duration
}

// New returns Handlers.
func New(d Deps) *Handlers {
	if d.Web == nil {
		d.Web = &http.Client{Timeout: thumbFetchTimeout}
	}
	if d.Log == nil {
		d.Log = slog.Default()
	}
	return &Handlers{d: d, keepAlive: keepAliveEvery}
}

// Runner returns the handler set for runner.New.
func (h *Handlers) Runner() runner.Handlers {
	return runner.Handlers{Resolve: h, Download: h, SendVideo: h, BotCall: h}
}

// taskError converts domain errors into the runner's wire error.
func taskError(err error) error {
	if err == nil {
		return nil
	}
	var te *telegram.Error
	if errors.As(err, &te) {
		code := te.Code()
		return &runner.TaskError{
			Code: string(code), Message: te.Error(), Retryable: code.Retryable(),
			TelegramCode: int32(te.Status), TelegramDescription: te.Description, //nolint:gosec // HTTP-like status
			RetryAfterS: int32(min(te.RetryAfter, 3600)), //nolint:gosec // bounded
			Cause:       err,
		}
	}
	var de *errs.Error
	if errors.As(err, &de) {
		return &runner.TaskError{Code: string(de.Code), Message: de.Error(), Retryable: de.Code.Retryable(), Cause: err}
	}
	return err
}

// progress reports a stage percentage now and then again every interval, so
// long downloads and uploads keep their lease even when nothing moves.
type progress struct {
	pct  atomic.Int32
	stop func()
}

func (h *Handlers) startProgress(ctx context.Context, prog runner.Progress) *progress {
	p := &progress{}
	ctx, cancel := context.WithCancel(ctx)
	p.stop = cancel
	go func() {
		t := time.NewTicker(h.keepAlive)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				prog(p.pct.Load())
			}
		}
	}()
	return p
}

// set records a new percentage and reports it when it moved by a step.
func (p *progress) set(prog runner.Progress, pct int32) {
	if old := p.pct.Swap(pct); pct/10 != old/10 {
		prog(pct)
	}
}
