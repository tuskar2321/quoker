// Package videos manages video metadata, deduplication and the resolve flow.
// See docs/02-architecture.md (resolve flow) and docs/05-data-model.md.
package videos

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"google.golang.org/protobuf/proto"

	linkv1 "github.com/tuskar2321/quoker/gen/quoker/link/v1"
	"github.com/tuskar2321/quoker/internal/core/queue"
	"github.com/tuskar2321/quoker/internal/core/store"
	"github.com/tuskar2321/quoker/internal/shared/errs"
	"github.com/tuskar2321/quoker/internal/shared/urls"
)

const (
	// videoTTL is how long metadata and the file live after last access.
	videoTTL = 24 * time.Hour
	// DefaultResolveWait is how long POST /resolve blocks waiting for the worker result.
	DefaultResolveWait = 15 * time.Second
	// resolvePoll is the interval between DB polls during the wait.
	resolvePoll = 300 * time.Millisecond
)

// FileStatus mirrors the DB check constraint.
type FileStatus string

// File statuses.
const (
	FileNone      FileStatus = "none"
	FilePreparing FileStatus = "preparing"
	FileReady     FileStatus = "ready"
	FileFailed    FileStatus = "failed"
)

// Video is a resolved video's metadata row.
type Video struct {
	ID           string
	Platform     string
	NativeID     string
	CanonicalURL string
	Author       string
	Caption      string
	DurationS    int
	SizeBytes    int64
	Width        int
	Height       int
	NoWatermark  bool
	HasThumbnail bool
	FileStatus   FileStatus
	FileSize     int64
	FileSHA256   string
	ResolvedAt   time.Time
	ExpiresAt    time.Time
	LastAccessAt time.Time
}

// Service provides video operations.
type Service struct {
	st          *store.Store
	q           *queue.Store
	now         func() time.Time
	resolveWait time.Duration
}

// New returns a Service.
func New(st *store.Store, q *queue.Store, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{st: st, q: q, now: now, resolveWait: DefaultResolveWait}
}

// WithResolveWait overrides the polling wait (for tests).
func (s *Service) WithResolveWait(d time.Duration) *Service {
	s.resolveWait = d
	return s
}

// Q exposes the queue store for tests.
func (s *Service) Q() *queue.Store { return s.q }

// ResolveResult is returned by Resolve.
type ResolveResult struct {
	Video  *Video // non-nil when resolved synchronously or from cache
	TaskID string // non-empty when queued and not yet done
}

// Resolve resolves a URL to a Video. It deduplicates by normalised URL and
// video_id. If the metadata is not in cache, it enqueues a resolve task and
// polls up to resolveWait for the result.
//
// The callerID (user_id) is recorded on the task for history; pass 0 for CLI.
func (s *Service) Resolve(ctx context.Context, rawURL string, callerID int64) (ResolveResult, error) {
	link, err := urls.Normalize(rawURL)
	if err != nil {
		return ResolveResult{}, err // already a typed errs.Code
	}

	// 1. Check the alias table (short links already resolved).
	if videoID, ok := s.lookupAlias(ctx, link.Key); ok {
		if v, err := s.touch(ctx, videoID); err == nil {
			return ResolveResult{Video: v}, nil
		}
		// Cache miss after alias hit: the video row expired; fall through to re-resolve.
	}

	// 2. Check by video_id when it is known without network (long-form URLs).
	if vid := link.VideoID(); vid != "" {
		if v, err := s.touch(ctx, vid); err == nil {
			return ResolveResult{Video: v}, nil
		}
	}

	// 3. Check if there is already an active resolve task for this key.
	dedupKey := "resolve:" + link.Key
	taskID, err := s.q.Enqueue(ctx, queue.EnqueueParams{
		Kind:     queue.KindResolve,
		Payload:  mustMarshal(&linkv1.ResolveTask{Url: link.URL, VideoId: link.VideoID()}),
		DedupKey: dedupKey,
		UserID:   callerID,
	})
	if err != nil && !errors.Is(err, queue.ErrDuplicate) {
		return ResolveResult{}, fmt.Errorf("enqueue resolve: %w", err)
	}

	// 4. Poll up to resolveWait for the task to complete.
	// time.After uses the real clock; deadline must too so they agree.
	deadline := time.Now().Add(s.resolveWait)
	for time.Now().Before(deadline) {
		t, err := s.q.Get(ctx, taskID)
		if err != nil {
			return ResolveResult{}, err
		}
		switch t.Status {
		case queue.StatusDone:
			// Apply the result that the worker stored.
			if v, err := s.touch(ctx, videoIDFromTask(t)); err == nil {
				// Record the alias so the same short link resolves instantly.
				_ = s.saveAlias(ctx, link.Key, v.ID)
				return ResolveResult{Video: v}, nil
			}
			// Worker completed but the row is not in DB yet (race): fall through.
		case queue.StatusFailed:
			return ResolveResult{}, workerTaskError(t)
		case queue.StatusCancelled:
			return ResolveResult{}, errs.New(errs.WorkerUnavailable, "resolve task cancelled")
		}
		select {
		case <-ctx.Done():
			return ResolveResult{}, ctx.Err()
		case <-time.After(resolvePoll):
		}
	}

	// 5. Timeout: return 202 with the task id.
	return ResolveResult{TaskID: taskID}, nil
}

// ApplyResolveResult is called by the link server after the worker completes a
// resolve task. It upserts the video row and records the alias.
func (s *Service) ApplyResolveResult(ctx context.Context, _ string, r *linkv1.ResolveResult, originalURL string) error {
	now := s.now()
	videoID := r.Platform + ":" + r.NativeId
	expires := now.Add(videoTTL)

	return s.st.Tx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO videos
			  (id, platform, native_id, canonical_url, author, caption,
			   duration_s, size_bytes, width, height, no_watermark, has_thumbnail,
			   file_status, resolved_at, expires_at, last_access_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,'none',?,?,?)
			ON CONFLICT(id) DO UPDATE SET
			  author        = excluded.author,
			  caption       = excluded.caption,
			  duration_s    = excluded.duration_s,
			  size_bytes    = excluded.size_bytes,
			  width         = excluded.width,
			  height        = excluded.height,
			  no_watermark  = excluded.no_watermark,
			  has_thumbnail = excluded.has_thumbnail,
			  canonical_url = excluded.canonical_url,
			  expires_at    = excluded.expires_at,
			  last_access_at= excluded.last_access_at`,
			videoID, r.Platform, r.NativeId, r.CanonicalUrl,
			nullStr(r.Author), nullStr(r.Caption),
			nullInt(int64(r.DurationS)), nullInt(r.SizeBytes),
			nullInt(int64(r.Width)), nullInt(int64(r.Height)),
			r.NoWatermark, r.ThumbnailUploaded,
			now.UnixMilli(), expires.UnixMilli(), now.UnixMilli())
		if err != nil {
			return fmt.Errorf("upsert video: %w", err)
		}
		if originalURL != "" {
			_, err = tx.ExecContext(ctx,
				`INSERT OR IGNORE INTO video_aliases (url_normalized, video_id, created_at) VALUES (?,?,?)`,
				originalURL, videoID, now.UnixMilli())
			if err != nil {
				return fmt.Errorf("insert alias: %w", err)
			}
		}
		// Record in history_videos (outlives the 24h cache, docs/05-data-model.md).
		_, err = tx.ExecContext(ctx, `
			INSERT INTO history_videos (video_id, platform, canonical_url, author, duration_s, size_bytes)
			VALUES (?,?,?,?,?,?)
			ON CONFLICT(video_id) DO UPDATE SET
			  author = excluded.author,
			  duration_s = excluded.duration_s,
			  size_bytes = excluded.size_bytes`,
			videoID, r.Platform, r.CanonicalUrl,
			nullStr(r.Author), nullInt(int64(r.DurationS)), nullInt(r.SizeBytes))
		if err != nil {
			return fmt.Errorf("upsert history_videos: %w", err)
		}
		return nil
	})
}

// Get returns a video by id, touching last_access_at.
func (s *Service) Get(ctx context.Context, id string) (*Video, error) {
	return s.touch(ctx, id)
}

// Task returns the current state of a task, optionally with the resolved Video.
type Task struct {
	ID       string
	Kind     string
	Status   string
	Progress int
	VideoID  string // non-empty when kind=resolve and status=done
	ErrCode  string
}

// GetTask returns a task row. When kind=resolve and status=done, it also
// returns the video.
func (s *Service) GetTask(ctx context.Context, taskID string) (*Task, *Video, error) {
	t, err := s.q.Get(ctx, taskID)
	if err != nil {
		return nil, nil, err
	}
	out := &Task{
		ID:       t.ID,
		Kind:     string(t.Kind),
		Status:   string(t.Status),
		Progress: t.Progress,
		ErrCode:  t.ErrorCode,
	}
	if t.Kind == queue.KindResolve && t.Status == queue.StatusDone {
		vid := videoIDFromTask(t)
		out.VideoID = vid
		if vid != "" {
			v, err := s.touch(ctx, vid)
			if err == nil {
				return out, v, nil
			}
		}
	}
	return out, nil, nil
}

// touch updates last_access_at and extends expires_at, then returns the video.
func (s *Service) touch(ctx context.Context, id string) (*Video, error) {
	now := s.now()
	newExpires := now.Add(videoTTL)
	res, err := s.st.DB().ExecContext(ctx,
		`UPDATE videos SET last_access_at = ?, expires_at = ? WHERE id = ?`,
		now.UnixMilli(), newExpires.UnixMilli(), id)
	if err != nil {
		return nil, fmt.Errorf("touch video: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, errs.New(errs.NotFound, "video not found")
	}
	return s.scan(ctx, id)
}

func (s *Service) scan(ctx context.Context, id string) (*Video, error) {
	var (
		v                                   Video
		platform, fileStatus                string
		author, caption, fileSHA256         sql.NullString
		durationS, sizeBytes, width, height sql.NullInt64
		fileSize                            sql.NullInt64
		noWatermark, hasThumbnail           sql.NullInt64
		resolvedMS, expiresMS, lastAccessMS int64
	)
	err := s.st.DB().QueryRowContext(ctx, `
		SELECT id, platform, native_id, canonical_url,
		       author, caption, duration_s, size_bytes, width, height,
		       no_watermark, has_thumbnail, file_status, file_size, file_sha256,
		       resolved_at, expires_at, last_access_at
		FROM videos WHERE id = ?`, id).Scan(
		&v.ID, &platform, &v.NativeID, &v.CanonicalURL,
		&author, &caption, &durationS, &sizeBytes, &width, &height,
		&noWatermark, &hasThumbnail, &fileStatus, &fileSize, &fileSHA256,
		&resolvedMS, &expiresMS, &lastAccessMS)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errs.New(errs.NotFound, "video not found")
	}
	if err != nil {
		return nil, fmt.Errorf("scan video: %w", err)
	}
	v.Platform = platform
	v.FileStatus = FileStatus(fileStatus)
	v.Author = author.String
	v.Caption = caption.String
	v.DurationS = int(durationS.Int64)
	v.SizeBytes = sizeBytes.Int64
	v.Width = int(width.Int64)
	v.Height = int(height.Int64)
	v.NoWatermark = noWatermark.Int64 != 0
	v.HasThumbnail = hasThumbnail.Int64 != 0
	v.FileSize = fileSize.Int64
	v.FileSHA256 = fileSHA256.String
	v.ResolvedAt = msTime(resolvedMS)
	v.ExpiresAt = msTime(expiresMS)
	v.LastAccessAt = msTime(lastAccessMS)
	return &v, nil
}

func (s *Service) lookupAlias(ctx context.Context, key string) (string, bool) {
	var videoID string
	err := s.st.DB().QueryRowContext(ctx,
		`SELECT video_id FROM video_aliases WHERE url_normalized = ?`, key).Scan(&videoID)
	return videoID, err == nil
}

func (s *Service) saveAlias(ctx context.Context, key, videoID string) error {
	_, err := s.st.DB().ExecContext(ctx,
		`INSERT OR IGNORE INTO video_aliases (url_normalized, video_id, created_at) VALUES (?,?,?)`,
		key, videoID, s.now().UnixMilli())
	return err
}

// videoIDFromTask reads the video_id from the task result blob.
// The result is a ResolveResult proto; if the blob is absent, falls back to
// re-deriving from error_code.
func videoIDFromTask(t *queue.Task) string {
	if len(t.Result) == 0 {
		return ""
	}
	var r linkv1.ResolveResult
	if err := proto.Unmarshal(t.Result, &r); err != nil {
		return ""
	}
	if r.Platform == "" || r.NativeId == "" {
		return ""
	}
	return r.Platform + ":" + r.NativeId
}

// workerTaskError maps a failed task's error_code to a typed errs.Error.
func workerTaskError(t *queue.Task) error {
	switch t.ErrorCode {
	case "private_video":
		return errs.New(errs.PrivateVideo, t.ErrorMsg)
	case "unsupported_content":
		return errs.New(errs.UnsupportedContent, t.ErrorMsg)
	case "login_required":
		return errs.New(errs.UpstreamError, "instagram login required")
	case "rate_limited":
		return errs.New(errs.RateLimited, t.ErrorMsg)
	case "too_large":
		return errs.New(errs.TooLarge, t.ErrorMsg)
	case "worker_unavailable", "lease_expired":
		return errs.New(errs.WorkerUnavailable, t.ErrorMsg)
	case "upstream_error", "internal":
		return errs.New(errs.UpstreamError, t.ErrorMsg)
	}
	return errs.New(errs.Internal, t.ErrorMsg)
}

func mustMarshal(m proto.Message) []byte {
	b, err := proto.Marshal(m)
	if err != nil {
		panic(fmt.Sprintf("marshal proto: %v", err))
	}
	return b
}

func nullStr(s string) sql.NullString { return sql.NullString{String: s, Valid: s != ""} }
func nullInt(v int64) sql.NullInt64   { return sql.NullInt64{Int64: v, Valid: v != 0} }
func msTime(ms int64) time.Time       { return time.UnixMilli(ms).UTC() }
