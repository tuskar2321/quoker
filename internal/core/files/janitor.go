package files

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// CleanupInterval is how often Run sweeps the cache (docs/05-data-model.md).
	CleanupInterval = 10 * time.Minute
	// staleAge is how old partial uploads and orphans must be before removal;
	// covers arrive before their video row, so fresh ones are left alone.
	staleAge = time.Hour
)

// Run sweeps the cache now and then every interval until ctx ends.
func (s *Store) Run(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		if err := s.Cleanup(ctx); err != nil && ctx.Err() == nil {
			s.log.Warn("cache cleanup", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Cleanup reconciles file states, deletes expired videos with their files,
// stale partial uploads and orphaned files, then evicts if the cache is over
// its limit.
func (s *Store) Cleanup(ctx context.Context) error {
	if _, err := s.Reconcile(ctx); err != nil {
		return err
	}
	now := s.now()
	ids, err := s.queryIDs(ctx, `SELECT id FROM videos WHERE expires_at < ?`, now.UnixMilli())
	if err != nil {
		return err
	}
	for _, id := range ids {
		if s.pinned(id) {
			continue
		}
		s.Discard(id)
		_ = os.Remove(s.thumbPath(id))
		if _, err := s.st.DB().ExecContext(ctx,
			`DELETE FROM videos WHERE id = ? AND expires_at < ?`, id, now.UnixMilli()); err != nil {
			return fmt.Errorf("delete expired video: %w", err)
		}
	}

	known := map[string]bool{}
	all, err := s.queryIDs(ctx, `SELECT id FROM videos`)
	if err != nil {
		return err
	}
	for _, id := range all {
		known[diskName(id)] = true
	}
	cutoff := now.Add(-staleAge)
	for _, dir := range []string{"files", "thumbs", "partial"} {
		entries, err := os.ReadDir(filepath.Join(s.root, dir))
		if err != nil {
			return fmt.Errorf("read %s: %w", dir, err)
		}
		for _, e := range entries {
			info, err := e.Info()
			if err != nil || info.ModTime().After(cutoff) {
				continue
			}
			name := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
			if dir == "partial" || !known[name] {
				_ = os.Remove(filepath.Join(s.root, dir, e.Name()))
			}
		}
	}
	return s.Evict(ctx)
}

// Evict removes the least recently accessed ready files until the cache is
// 10% under its limit. Files being served are skipped.
func (s *Store) Evict(ctx context.Context) error {
	used, err := s.usedBytes()
	if err != nil || used <= s.maxBytes {
		return err
	}
	target := s.maxBytes - s.maxBytes/10
	ids, err := s.queryIDs(ctx, `SELECT id FROM videos WHERE file_status = 'ready' ORDER BY last_access_at`)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if used <= target {
			break
		}
		if s.pinned(id) {
			continue
		}
		st, err := os.Stat(s.filePath(id))
		if err == nil {
			if err := os.Remove(s.filePath(id)); err != nil {
				return fmt.Errorf("evict: %w", err)
			}
			used -= st.Size()
		}
		if _, err := s.st.DB().ExecContext(ctx,
			`UPDATE videos SET file_status = 'none', file_size = NULL, file_sha256 = NULL WHERE id = ?`, id); err != nil {
			return fmt.Errorf("mark evicted: %w", err)
		}
		s.log.Info("evicted cached file", "video_id", id)
	}
	return nil
}

// UsedBytes returns the bytes the cache occupies on disk.
func (s *Store) UsedBytes() (int64, error) { return s.usedBytes() }

// MaxBytes returns the cache limit.
func (s *Store) MaxBytes() int64 { return s.maxBytes }

// Root returns the cache directory.
func (s *Store) Root() string { return s.root }

func (s *Store) usedBytes() (int64, error) {
	var total int64
	for _, dir := range []string{"files", "thumbs", "partial"} {
		entries, err := os.ReadDir(filepath.Join(s.root, dir))
		if err != nil {
			return 0, fmt.Errorf("read %s: %w", dir, err)
		}
		for _, e := range entries {
			if info, err := e.Info(); err == nil {
				total += info.Size()
			}
		}
	}
	return total, nil
}

func (s *Store) queryIDs(ctx context.Context, q string, args ...any) ([]string, error) {
	rows, err := s.st.DB().QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list videos: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan video id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// Reconcile fixes file states the disk or the queue no longer back
// (docs/11-nfr-security.md): "ready" without a file on disk and "preparing"
// without an active download task become "none". It runs at startup, so a
// damaged cache or a lost queue does not leave videos stuck, and with every
// cleanup.
func (s *Store) Reconcile(ctx context.Context) (fixed int, err error) {
	ready, err := s.queryIDs(ctx, `SELECT id FROM videos WHERE file_status = 'ready'`)
	if err != nil {
		return 0, err
	}
	var gone []string
	for _, id := range ready {
		if !s.HasFile(id) && !s.pinned(id) {
			gone = append(gone, id)
		}
	}
	stuck, err := s.queryIDs(ctx, `
		SELECT v.id FROM videos v
		WHERE v.file_status = 'preparing' AND NOT EXISTS (
			SELECT 1 FROM tasks t
			WHERE t.dedup_key = 'download:' || v.id AND t.status IN ('queued', 'leased'))`)
	if err != nil {
		return 0, err
	}
	for _, id := range append(gone, stuck...) {
		if _, err := s.st.DB().ExecContext(ctx,
			`UPDATE videos SET file_status = 'none', file_size = NULL, file_sha256 = NULL WHERE id = ?`, id); err != nil {
			return fixed, fmt.Errorf("reset file state: %w", err)
		}
		fixed++
	}
	if fixed > 0 {
		s.log.Info("file states reconciled", "reset", fixed)
	}
	return fixed, nil
}
