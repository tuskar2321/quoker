// Package backup writes consistent copies of the core SQLite database with
// VACUUM INTO and keeps the newest few. See docs/10-deploy.md (backups).
package backup

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const (
	// Keep is how many backups stay in the directory.
	Keep = 7
	// RunTimeout bounds one backup.
	RunTimeout = 5 * time.Minute

	filePrefix  = "quoker-"
	fileSuffix  = ".db"
	tmpPrefix   = ".quoker-tmp-"
	stampLayout = "20060102-150405"
)

// Backuper copies the database into Dir.
type Backuper struct {
	db   *sql.DB
	dir  string
	keep int
	now  func() time.Time
}

// New returns a Backuper; now defaults to time.Now. File names carry UTC time.
func New(db *sql.DB, dir string, now func() time.Time) *Backuper {
	if now == nil {
		now = time.Now
	}
	return &Backuper{db: db, dir: dir, keep: Keep, now: now}
}

// Run writes quoker-<YYYYMMDD-HHMMSS>.db (UTC) and prunes older backups.
// The copy is built under a temporary name and renamed, so a half-written
// file never matches quoker-*.db. It is safe while the server runs: VACUUM INTO
// reads a consistent snapshot through its own connection (WAL mode).
func (b *Backuper) Run(ctx context.Context) (string, error) {
	if err := os.MkdirAll(b.dir, 0o750); err != nil {
		return "", fmt.Errorf("create backup dir: %w", err)
	}
	b.removeStaleTmp()
	final := filepath.Join(b.dir, filePrefix+b.now().UTC().Format(stampLayout)+fileSuffix)
	tmp := filepath.Join(b.dir, tmpPrefix+filepath.Base(final))
	_ = os.Remove(tmp)

	if _, err := b.db.ExecContext(ctx, "VACUUM INTO ?", tmp); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("vacuum into: %w", err)
	}
	// 0640: the snapshot holds secrets (bot token, cookies), so no world access, but the
	// `quoker` group is readable so deploy/pc/backup-pull.sh can rsync it as `deploy`.
	if err := os.Chmod(tmp, 0o640); err != nil { //nolint:gosec // G302: group read is required, see above
		_ = os.Remove(tmp)
		return "", fmt.Errorf("chmod backup: %w", err)
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("publish backup: %w", err)
	}
	if err := b.prune(); err != nil {
		return final, fmt.Errorf("prune backups: %w", err)
	}
	return final, nil
}

func (b *Backuper) removeStaleTmp() {
	old, _ := filepath.Glob(filepath.Join(b.dir, tmpPrefix+"*"))
	for _, p := range old {
		_ = os.Remove(p)
	}
}

// prune deletes all but the newest keep backups. Names sort by time.
func (b *Backuper) prune() error {
	files, err := filepath.Glob(filepath.Join(b.dir, filePrefix+"*"+fileSuffix))
	if err != nil {
		return err
	}
	sort.Strings(files)
	var errs []error
	for len(files) > b.keep {
		if err := os.Remove(files[0]); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
		files = files[1:]
	}
	return errors.Join(errs...)
}

// NextRun returns the first 04:00 (in loc) strictly after now.
func NextRun(now time.Time, loc *time.Location) time.Time {
	n := now.In(loc)
	t := time.Date(n.Year(), n.Month(), n.Day(), 4, 0, 0, 0, loc)
	if !t.After(n) {
		t = time.Date(n.Year(), n.Month(), n.Day()+1, 4, 0, 0, 0, loc)
	}
	return t
}

// Scheduler runs a backup every day at 04:00 in Loc.
type Scheduler struct {
	Backup *Backuper
	Loc    *time.Location
	Log    *slog.Logger
	// Now and Sleep are replaceable in tests; Sleep returns ctx.Err() when
	// the context ends first.
	Now   func() time.Time
	Sleep func(ctx context.Context, d time.Duration) error
}

// Run blocks until ctx ends.
func (s *Scheduler) Run(ctx context.Context) {
	now, sleep := s.Now, s.Sleep
	if now == nil {
		now = time.Now
	}
	if sleep == nil {
		sleep = realSleep
	}
	for {
		next := NextRun(now(), s.Loc)
		if err := sleep(ctx, next.Sub(now())); err != nil {
			return
		}
		runCtx, cancel := context.WithTimeout(ctx, RunTimeout)
		path, err := s.Backup.Run(runCtx)
		cancel()
		if err != nil {
			s.Log.Error("scheduled backup failed", "err", err)
		} else {
			s.Log.Info("backup written", "path", path)
		}
	}
}

func realSleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
