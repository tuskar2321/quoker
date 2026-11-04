package tasks

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"

	linkv1 "github.com/tuskar2321/quoker/gen/quoker/link/v1"
	"github.com/tuskar2321/quoker/internal/shared/errs"
	"github.com/tuskar2321/quoker/internal/worker/runner"
	"github.com/tuskar2321/quoker/internal/worker/telegram"
	"github.com/tuskar2321/quoker/internal/worker/tmpcache"
	"github.com/tuskar2321/quoker/internal/worker/ytdlp"
)

const (
	defaultDownloadBytes = 500 << 20
	defaultSendBytes     = 50 << 20

	// maxBotFileBytes caps files fetched for the core, e.g. cookies.txt.
	maxBotFileBytes = 1 << 20

	variantPhone    = "phone"
	variantTelegram = "tg"
)

// fileMeta is stored next to a cached file.
type fileMeta struct {
	Format    string `json:"format"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	SHA256    string `json:"sha256"`
	Thumbnail string `json:"thumbnail"`
}

func readMeta(h *tmpcache.Handle) fileMeta {
	var m fileMeta
	_ = json.Unmarshal(h.Meta, &m)
	return m
}

// Download prepares the phone file and uploads it to the core.
func (h *Handlers) Download(ctx context.Context, task *linkv1.DownloadTask, cfg *linkv1.Config, prog runner.Progress) (*linkv1.DownloadResult, error) {
	if cfg.UploadBase == "" {
		return nil, taskError(errs.New(errs.Internal, "upload_base is not configured"))
	}
	p := h.startProgress(ctx, prog)
	defer p.stop()

	maxBytes := limit(task.MaxBytes, cfg.MaxDownloadBytes, defaultDownloadBytes)
	f, ok := h.d.Cache.Get(cacheKey(task.VideoId, variantPhone))
	if !ok {
		var err error
		if f, err = h.fetch(ctx, task.VideoId, task.Url, cfg.InstagramCookies, maxBytes, variantPhone, p, prog); err != nil {
			return nil, taskError(err)
		}
	}
	defer f.Release()
	p.set(prog, 50)

	err := h.d.Upload.PutFile(ctx, cfg.UploadBase, task.VideoId, f.Path, func(sent, total int64) {
		p.set(prog, 50+int32(sent*50/total)) //nolint:gosec // 0..50
	})
	if err != nil {
		return nil, taskError(err)
	}
	m := readMeta(f)
	return &linkv1.DownloadResult{SizeBytes: f.Size, Sha256: m.SHA256, FormatId: m.Format}, nil
}

// SendVideo posts the video to the chat, reusing a cached file when one fits.
func (h *Handlers) SendVideo(ctx context.Context, task *linkv1.SendVideoTask, cfg *linkv1.Config, prog runner.Progress) (*linkv1.SendVideoResult, error) {
	p := h.startProgress(ctx, prog)
	defer p.stop()

	maxBytes := limit(task.MaxBytes, cfg.MaxSendBytes, defaultSendBytes)
	f, ok := h.d.Cache.Get(cacheKey(task.VideoId, variantTelegram))
	if !ok {
		if f, ok = h.d.Cache.Get(cacheKey(task.VideoId, variantPhone)); ok && f.Size > maxBytes {
			f.Release()
			ok = false
		}
	}
	if !ok {
		var err error
		if f, err = h.fetch(ctx, task.VideoId, task.Url, cfg.InstagramCookies, maxBytes, variantTelegram, p, prog); err != nil {
			return nil, taskError(err)
		}
	}
	defer f.Release()
	p.set(prog, 50)

	m := readMeta(f)
	var thumb []byte
	if m.Thumbnail != "" {
		var err error
		if thumb, err = h.thumbnail(ctx, m.Thumbnail, previewSide, previewBytes); err != nil {
			h.d.Log.Warn("send without preview", "video_id", task.VideoId, "err", err)
		}
	}
	width, height := m.Width, m.Height
	if width == 0 || height == 0 {
		width, height = int(task.Width), int(task.Height)
	}
	msgID, err := h.d.Telegram.SendVideo(ctx, cfg.BotApiBase, cfg.BotToken, telegram.Video{
		ChatID:      task.ChatId,
		CaptionHTML: task.CaptionHtml,
		Path:        f.Path,
		Thumb:       thumb,
		DurationS:   int(task.DurationS),
		Width:       width,
		Height:      height,
		Progress: func(sent, total int64) {
			p.set(prog, 50+int32(sent*49/total)) //nolint:gosec // 0..49
		},
	})
	if err != nil {
		return nil, taskError(err)
	}
	return &linkv1.SendVideoResult{MessageId: msgID, SentBytes: f.Size, FormatId: m.Format}, nil
}

// fetch downloads the best format that fits maxBytes and caches it. For
// Telegram it steps down the resolution ladder when the real file is larger
// than estimated; the phone file gets a single attempt.
func (h *Handlers) fetch(ctx context.Context, videoID, url, cookies string, maxBytes int64, variant string, p *progress, prog runner.Progress) (*tmpcache.Handle, error) {
	if !validVideoID.MatchString(videoID) {
		return nil, errs.New(errs.Internal, "malformed video_id")
	}
	info, err := h.d.YtDlp.Metadata(ctx, url, cookies)
	if err != nil {
		return nil, err
	}
	if err := checkContent(info); err != nil {
		return nil, err
	}
	platform, _ := splitVideoID(videoID)
	noWM := platform == "tiktok"
	if noWM && !info.HasCleanVideo() {
		return nil, errs.New(errs.UpstreamError, "tiktok offers only watermarked formats")
	}
	p.set(prog, 10)

	for height := ytdlp.Ladder[0]; height > 0; {
		c, ok := info.Choose(height, maxBytes, noWM)
		if !ok {
			break
		}
		f, size, err := h.download(ctx, videoID, url, cookies, variant, c, info.Thumbnail, maxBytes)
		if err != nil || f != nil {
			return f, err
		}
		h.d.Log.Info("format larger than limit", "video_id", videoID, "format", c.String(), "bytes", size)
		if variant != variantTelegram || c.Height == 0 {
			break
		}
		height = ytdlp.NextHeight(min(height, c.Height))
	}
	return nil, errs.New(errs.TooLarge, fmt.Sprintf("no format up to %d bytes", maxBytes))
}

// download fetches one choice; it returns a nil handle and the real size when
// the file turned out larger than maxBytes.
func (h *Handlers) download(ctx context.Context, videoID, url, cookies, variant string, c ytdlp.Choice, thumbURL string, maxBytes int64) (*tmpcache.Handle, int64, error) {
	dir, err := h.d.Cache.StagingDir()
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = os.RemoveAll(dir) }()

	path, err := h.d.YtDlp.Download(ctx, url, c.Selector, dir, cookies, maxBytes)
	if errs.Is(err, errs.TooLarge) {
		return nil, maxBytes + 1, nil // too big: the ladder tries a lower resolution
	}
	if err != nil {
		return nil, 0, err
	}
	sum, size, err := hashFile(path)
	if err != nil {
		return nil, 0, err
	}
	if size > maxBytes {
		return nil, size, nil
	}
	meta, _ := json.Marshal(fileMeta{Format: c.Selector, Width: c.Width, Height: c.Height, SHA256: sum, Thumbnail: thumbURL})
	f, err := h.d.Cache.Put(cacheKey(videoID, variant), path, meta)
	return f, size, err
}

// BotCall performs an arbitrary Bot API method for the core.
func (h *Handlers) BotCall(ctx context.Context, task *linkv1.BotCallTask, cfg *linkv1.Config) (*linkv1.BotCallResult, error) {
	if !validMethod.MatchString(task.Method) {
		return nil, &runner.TaskError{Code: string(errs.Internal), Message: "invalid bot method"}
	}
	ctx, cancel := context.WithTimeout(ctx, botCallTimeout)
	defer cancel()
	if task.DownloadFilePath != "" {
		b, err := h.d.Telegram.DownloadFile(ctx, cfg.BotApiBase, cfg.BotToken, task.DownloadFilePath, maxBotFileBytes)
		if err != nil {
			return nil, taskError(err)
		}
		return &linkv1.BotCallResult{FileContent: b}, nil
	}
	raw, err := h.d.Telegram.Call(ctx, cfg.BotApiBase, cfg.BotToken, task.Method, []byte(task.ParamsJson))
	if err != nil {
		return nil, taskError(err)
	}
	return &linkv1.BotCallResult{ResponseJson: string(raw)}, nil
}

var (
	validMethod  = regexp.MustCompile(`^[A-Za-z]{1,64}$`)
	validVideoID = regexp.MustCompile(`^(tiktok|instagram):[A-Za-z0-9_-]{1,64}$`)
)

func cacheKey(videoID, variant string) string { return videoID + "/" + variant }

func limit(task, cfg, def int64) int64 {
	switch {
	case task > 0:
		return task
	case cfg > 0:
		return cfg
	}
	return def
}

func hashFile(path string) (sum string, size int64, err error) {
	f, err := os.Open(path) //nolint:gosec // path produced by yt-dlp in our staging dir
	if err != nil {
		return "", 0, fmt.Errorf("open download: %w", err)
	}
	defer func() { _ = f.Close() }()
	hash := sha256.New()
	n, err := io.Copy(hash, f)
	if err != nil {
		return "", 0, fmt.Errorf("hash download: %w", err)
	}
	return hex.EncodeToString(hash.Sum(nil)), n, nil
}
