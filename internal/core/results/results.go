// Package results applies worker task outcomes to the domain services.
package results

import (
	"context"
	"fmt"
	"strings"

	"google.golang.org/protobuf/proto"

	linkv1 "github.com/tuskar2321/quoker/gen/quoker/link/v1"
	"github.com/tuskar2321/quoker/internal/core/files"
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
	files  *files.Store
}

// New returns a Handler.
func New(v *videos.Service, s *sends.Service, f *files.Store) *Handler {
	return &Handler{videos: v, sends: s, files: f}
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
	case queue.KindDownload:
		res := r.GetDownload()
		if res == nil {
			return fmt.Errorf("download task %s: unexpected result type", t.ID)
		}
		id, err := downloadVideoID(t)
		if err != nil {
			return err
		}
		if err := h.files.Verify(id, res.SizeBytes, res.Sha256); err != nil {
			_ = h.videos.SetFileFailed(ctx, id)
			return err
		}
		return h.videos.SetFileReady(ctx, id, res.SizeBytes, res.Sha256)
	}
	return nil
}

// TaskFailed applies a final failure.
func (h *Handler) TaskFailed(ctx context.Context, t *queue.Task) error {
	switch t.Kind {
	case queue.KindSendVideo:
		return h.sends.MarkFailed(ctx, t.ID, t.ErrorCode)
	case queue.KindDownload:
		id, err := downloadVideoID(t)
		if err != nil {
			return err
		}
		h.files.Discard(id)
		return h.videos.SetFileFailed(ctx, id)
	}
	return nil
}

func downloadVideoID(t *queue.Task) (string, error) {
	var p linkv1.DownloadTask
	if err := proto.Unmarshal(t.Payload, &p); err != nil {
		return "", fmt.Errorf("download task %s: decode payload: %w", t.ID, err)
	}
	return p.VideoId, nil
}
