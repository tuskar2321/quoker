package tasks

import (
	"context"
	"fmt"
	"math"
	"strings"

	linkv1 "github.com/tuskar2321/quoker/gen/quoker/link/v1"
	"github.com/tuskar2321/quoker/internal/shared/errs"
	"github.com/tuskar2321/quoker/internal/worker/runner"
	"github.com/tuskar2321/quoker/internal/worker/ytdlp"
)

// Resolve fetches metadata and uploads the cover. A failed cover upload does
// not fail the task: the video is still usable without it.
func (h *Handlers) Resolve(ctx context.Context, task *linkv1.ResolveTask, cfg *linkv1.Config, _ runner.Progress) (*linkv1.ResolveResult, error) {
	info, err := h.d.YtDlp.Metadata(ctx, task.Url, cfg.InstagramCookies)
	if err != nil {
		return nil, taskError(err)
	}
	if err := checkContent(info); err != nil {
		return nil, taskError(err)
	}
	platform, nativeID := splitVideoID(task.VideoId)
	if platform == "" {
		platform, nativeID = info.Platform(), info.ID
	}
	if platform == "" || nativeID == "" {
		return nil, taskError(errs.New(errs.UpstreamError, "unknown extractor "+info.ExtractorKey))
	}
	videoID := platform + ":" + nativeID

	res := &linkv1.ResolveResult{
		Platform:     platform,
		NativeId:     nativeID,
		CanonicalUrl: firstNonEmpty(info.WebpageURL, task.Url),
		Author:       author(info, platform),
		Caption:      strings.TrimSpace(info.Description),
		DurationS:    int32(math.Round(info.Duration)),
		Width:        int32(info.Width),
		Height:       int32(info.Height),
		NoWatermark:  platform == "tiktok" && info.HasCleanVideo(),
		UsedCookies:  info.UsedCookies,
	}
	if c, ok := info.Choose(ytdlp.Ladder[0], limit(0, cfg.MaxDownloadBytes, defaultDownloadBytes), platform == "tiktok"); ok {
		res.SizeBytes = c.EstBytes
		if c.Height > 0 {
			res.Width, res.Height = int32(c.Width), int32(c.Height) //nolint:gosec // pixels
		}
	}

	if cfg.UploadBase != "" {
		cover, err := h.thumbnail(ctx, info.Thumbnail, coverSide, coverBytes)
		if err == nil {
			err = h.d.Upload.PutThumb(ctx, cfg.UploadBase, videoID, cover)
		}
		if err != nil {
			h.d.Log.Warn("cover not uploaded", "video_id", videoID, "err", err)
		} else {
			res.ThumbnailUploaded = true
		}
	}
	return res, nil
}

func checkContent(info *ytdlp.Info) error {
	switch {
	case info.Type == "playlist" || info.Type == "multi_video":
		return errs.New(errs.UnsupportedContent, "carousel or playlist")
	case !info.HasVideo():
		return errs.New(errs.UnsupportedContent, "no video formats")
	case info.Duration > maxDurationS:
		return errs.New(errs.UnsupportedContent, fmt.Sprintf("video is %.0f s long", info.Duration))
	}
	return nil
}

// splitVideoID splits "<platform>:<native_id>"; empty when unknown.
func splitVideoID(id string) (platform, nativeID string) {
	p, n, ok := strings.Cut(id, ":")
	if !ok || p == "" || n == "" {
		return "", ""
	}
	return p, n
}

// author prefers the handle: TikTok puts it in uploader, Instagram in channel.
func author(info *ytdlp.Info, platform string) string {
	if platform == "instagram" {
		return firstNonEmpty(info.Channel, info.UploaderID, info.Uploader)
	}
	return firstNonEmpty(info.Uploader, info.UploaderID, info.Channel)
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}
