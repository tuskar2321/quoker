// Package results applies worker task outcomes to the domain services.
package results

import (
	"context"
	"fmt"
	"strings"

	linkv1 "github.com/tuskar2321/quoker/gen/quoker/link/v1"
	"github.com/tuskar2321/quoker/internal/core/queue"
	"github.com/tuskar2321/quoker/internal/core/sends"
	"github.com/tuskar2321/quoker/internal/core/videos"
)

// resolveDedupPrefix matches the dedup key built by videos.Resolve.
const resolveDedupPrefix = "resolve:"

// Handler implements link.ResultHandler.
type Handler struct {
	videos *videos.Service
	sends  *sends.Service
}

// New returns a Handler.
func New(v *videos.Service, s *sends.Service) *Handler {
	return &Handler{videos: v, sends: s}
}

// TaskSucceeded applies a successful result.
func (h *Handler) TaskSucceeded(ctx context.Context, t *queue.Task, r *linkv1.TaskResult) error {
	switch t.Kind {
	case queue.KindResolve:
		res := r.GetResolve()
		if res == nil {
			return fmt.Errorf("resolve task %s: unexpected result type", t.ID)
		}
		alias := strings.TrimPrefix(t.DedupKey, resolveDedupPrefix)
		return h.videos.ApplyResolveResult(ctx, t.ID, res, alias)
	case queue.KindSendVideo:
		res := r.GetSendVideo()
		if res == nil {
			return fmt.Errorf("send_video task %s: unexpected result type", t.ID)
		}
		return h.sends.MarkSent(ctx, t.ID, res.MessageId)
	}
	return nil
}

// TaskFailed applies a final failure.
func (h *Handler) TaskFailed(ctx context.Context, t *queue.Task) error {
	if t.Kind == queue.KindSendVideo {
		return h.sends.MarkFailed(ctx, t.ID, t.ErrorCode)
	}
	return nil
}
