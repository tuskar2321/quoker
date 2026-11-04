// Package history serves the user's action history: one entry per video with
// its actions, newest first (docs/01-product.md F-23, docs/03-public-api.md).
package history

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/tuskar2321/quoker/internal/core/store"
	"github.com/tuskar2321/quoker/internal/core/videos"
	"github.com/tuskar2321/quoker/internal/shared/errs"
)

const (
	// MaxActions is how far back the history reaches (F-23).
	MaxActions = 200
	// DefaultLimit is the page size when the client sends none.
	DefaultLimit = 20
	// MaxLimit is the largest page.
	MaxLimit = 50
	// Retention is how long actions are kept (docs/11-nfr-security.md).
	Retention = 90 * 24 * time.Hour
	// CleanupInterval is how often Run prunes old entries.
	CleanupInterval = time.Hour
	// resolvedEvery limits "resolved" entries per video, so opening the same
	// link again does not flood the history.
	resolvedEvery = 24 * time.Hour
)

// Action is one thing the user did with a video.
type Action struct {
	Type        string // resolved, downloaded, sent, send_failed
	TargetTitle string
	SendID      int64
	At          time.Time
}

// Item groups a video's actions.
type Item struct {
	ID      int64 // id of the newest action, stable across pages
	Video   *videos.Video
	Actions []Action
}

// Page is one page of the history.
type Page struct {
	Items      []Item
	NextCursor string // empty on the last page
}

// Service reads and maintains the history.
type Service struct {
	st  *store.Store
	now func() time.Time
}

// New returns a Service.
func New(st *store.Store, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{st: st, now: now}
}

// RecordResolved notes that the user opened a video, at most once a day.
func (s *Service) RecordResolved(ctx context.Context, userID int64, videoID string) error {
	now := s.now()
	_, err := s.st.DB().ExecContext(ctx, `
		INSERT INTO history (user_id, video_id, action, created_at)
		SELECT ?, ?, 'resolved', ?
		WHERE NOT EXISTS (
			SELECT 1 FROM history
			WHERE user_id = ? AND video_id = ? AND action = 'resolved' AND created_at >= ?)`,
		userID, videoID, now.UnixMilli(), userID, videoID, now.Add(-resolvedEvery).UnixMilli())
	if err != nil {
		return fmt.Errorf("record resolved: %w", err)
	}
	return nil
}

// List returns a page of the user's history. cursor is the NextCursor of
// the previous page, or empty for the first.
func (s *Service) List(ctx context.Context, userID int64, cursor string, limit int) (Page, error) {
	if limit <= 0 {
		limit = DefaultLimit
	}
	limit = min(limit, MaxLimit)
	afterAt, afterID, err := decodeCursor(cursor)
	if err != nil {
		return Page{}, err
	}

	heads, err := s.heads(ctx, userID, afterAt, afterID, limit+1)
	if err != nil {
		return Page{}, err
	}

	var page Page
	if len(heads) > limit {
		heads = heads[:limit]
		last := heads[len(heads)-1]
		page.NextCursor = encodeCursor(last.lastAt, last.lastID)
	}
	if len(heads) == 0 {
		return page, nil
	}
	ids := make([]string, len(heads))
	for i, h := range heads {
		ids[i] = h.videoID
	}
	actions, err := s.actions(ctx, userID, ids)
	if err != nil {
		return Page{}, err
	}
	for _, h := range heads {
		v, err := s.video(ctx, h.videoID)
		if err != nil {
			return Page{}, err
		}
		page.Items = append(page.Items, Item{ID: h.lastID, Video: v, Actions: actions[h.videoID]})
	}
	return page, nil
}

type head struct {
	videoID string
	lastAt  int64
	lastID  int64
}

// heads lists the videos among the newest MaxActions actions, ordered by
// each video's latest action, after the cursor position.
func (s *Service) heads(ctx context.Context, userID, afterAt, afterID int64, n int) ([]head, error) {
	rows, err := s.st.DB().QueryContext(ctx, `
		WITH recent AS (
			SELECT id, video_id, created_at FROM history
			WHERE user_id = ? AND created_at >= ?
			ORDER BY created_at DESC, id DESC LIMIT ?)
		SELECT video_id, MAX(created_at) AS last_at, MAX(id) AS last_id
		FROM recent GROUP BY video_id
		HAVING ? = 0 OR (last_at, last_id) < (?, ?)
		ORDER BY last_at DESC, last_id DESC
		LIMIT ?`,
		userID, s.now().Add(-Retention).UnixMilli(), MaxActions,
		afterAt, afterAt, afterID, n)
	if err != nil {
		return nil, fmt.Errorf("list history: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []head
	for rows.Next() {
		var h head
		if err := rows.Scan(&h.videoID, &h.lastAt, &h.lastID); err != nil {
			return nil, fmt.Errorf("scan history: %w", err)
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// actions loads the recent actions of the given videos, newest first.
func (s *Service) actions(ctx context.Context, userID int64, videoIDs []string) (map[string][]Action, error) {
	args := []any{userID, s.now().Add(-Retention).UnixMilli(), MaxActions}
	for _, id := range videoIDs {
		args = append(args, id)
	}
	rows, err := s.st.DB().QueryContext(ctx, `
		WITH recent AS (
			SELECT * FROM history WHERE user_id = ? AND created_at >= ?
			ORDER BY created_at DESC, id DESC LIMIT ?)
		SELECT video_id, action, target_title, send_id, created_at FROM recent
		WHERE video_id IN (`+placeholders(len(videoIDs))+`)
		ORDER BY created_at DESC, id DESC`, args...)
	if err != nil {
		return nil, fmt.Errorf("list actions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string][]Action{}
	for rows.Next() {
		var (
			videoID string
			a       Action
			title   sql.NullString
			sendID  sql.NullInt64
			atMS    int64
		)
		if err := rows.Scan(&videoID, &a.Type, &title, &sendID, &atMS); err != nil {
			return nil, fmt.Errorf("scan action: %w", err)
		}
		a.TargetTitle, a.SendID, a.At = title.String, sendID.Int64, time.UnixMilli(atMS).UTC()
		out[videoID] = append(out[videoID], a)
	}
	return out, rows.Err()
}

// video prefers the live cache row (thumbnail, file state, caption) and
// falls back to the long-lived history copy once the cache forgot it.
func (s *Service) video(ctx context.Context, id string) (*videos.Video, error) {
	var (
		v                  videos.Video
		author, caption    sql.NullString
		duration, size     sql.NullInt64
		width, height, nwm sql.NullInt64
		hasThumb           sql.NullInt64
		fileStatus         sql.NullString
		fileSize           sql.NullInt64
	)
	err := s.st.DB().QueryRowContext(ctx, `
		SELECT COALESCE(v.platform, h.platform), COALESCE(v.native_id, ''), COALESCE(v.canonical_url, h.canonical_url),
		       COALESCE(v.author, h.author), v.caption, COALESCE(v.duration_s, h.duration_s),
		       COALESCE(v.size_bytes, h.size_bytes), v.width, v.height, v.no_watermark,
		       v.has_thumbnail, v.file_status, v.file_size
		FROM (SELECT ? AS id) k
		LEFT JOIN videos v ON v.id = k.id
		LEFT JOIN history_videos h ON h.video_id = k.id
		WHERE v.id IS NOT NULL OR h.video_id IS NOT NULL`, id).Scan(
		&v.Platform, &v.NativeID, &v.CanonicalURL, &author, &caption, &duration, &size,
		&width, &height, &nwm, &hasThumb, &fileStatus, &fileSize)
	if errors.Is(err, sql.ErrNoRows) {
		// Metadata vanished entirely; keep the entry usable with the id alone.
		platform, _, _ := strings.Cut(id, ":")
		return &videos.Video{ID: id, Platform: platform, FileStatus: videos.FileNone}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("history video: %w", err)
	}
	v.ID = id
	v.Author, v.Caption = author.String, caption.String
	v.DurationS, v.SizeBytes = int(duration.Int64), size.Int64
	v.Width, v.Height = int(width.Int64), int(height.Int64)
	v.NoWatermark = nwm.Int64 != 0
	v.HasThumbnail = hasThumb.Int64 != 0
	v.FileStatus = videos.FileNone
	if fileStatus.Valid {
		v.FileStatus = videos.FileStatus(fileStatus.String)
	}
	v.FileSize = fileSize.Int64
	return &v, nil
}

// Cleanup drops actions older than Retention and history metadata no entry
// or cache row needs any more.
func (s *Service) Cleanup(ctx context.Context) error {
	cutoff := s.now().Add(-Retention).UnixMilli()
	if _, err := s.st.DB().ExecContext(ctx, `DELETE FROM history WHERE created_at < ?`, cutoff); err != nil {
		return fmt.Errorf("prune history: %w", err)
	}
	_, err := s.st.DB().ExecContext(ctx, `
		DELETE FROM history_videos
		WHERE video_id NOT IN (SELECT video_id FROM history)
		  AND video_id NOT IN (SELECT id FROM videos)`)
	if err != nil {
		return fmt.Errorf("prune history videos: %w", err)
	}
	return nil
}

// Run calls Cleanup now and every interval until ctx ends.
func (s *Service) Run(ctx context.Context, interval time.Duration, onErr func(error)) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		if err := s.Cleanup(ctx); err != nil && ctx.Err() == nil && onErr != nil {
			onErr(err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

func encodeCursor(at, id int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(at, 10) + ":" + strconv.FormatInt(id, 10)))
}

func decodeCursor(c string) (at, id int64, err error) {
	if c == "" {
		return 0, 0, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(c)
	if err == nil {
		a, b, ok := strings.Cut(string(raw), ":")
		if ok {
			if at, err = strconv.ParseInt(a, 10, 64); err == nil {
				if id, err = strconv.ParseInt(b, 10, 64); err == nil && at > 0 {
					return at, id, nil
				}
			}
		}
	}
	return 0, 0, errs.New(errs.InvalidRequest, "bad cursor")
}
