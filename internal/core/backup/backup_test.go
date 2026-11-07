package backup_test

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tuskar2321/quoker/internal/core/backup"
	"github.com/tuskar2321/quoker/internal/core/store"
)

func openStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "q.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func TestRunWritesConsistentCopyAndPrunes(t *testing.T) {
	ctx := context.Background()
	st := openStore(t)
	if _, err := st.DB().ExecContext(ctx, `INSERT INTO workers (id, fingerprint, status, created_at) VALUES ('w1','fp','allowed',0)`); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "backups")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	// A leftover temp file and an unrelated file must not count as backups.
	stale := filepath.Join(dir, ".quoker-tmp-quoker-20200101-000000.db")
	other := filepath.Join(dir, "notes.db")
	for _, p := range []string{stale, other} {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	clock := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	b := backup.New(st.DB(), dir, func() time.Time { return clock })

	var last string
	for i := 0; i < 9; i++ {
		clock = clock.Add(time.Hour)
		p, err := b.Run(ctx)
		if err != nil {
			t.Fatal(err)
		}
		last = p
	}
	if want := filepath.Join(dir, "quoker-20260101-090000.db"); last != want {
		t.Errorf("path = %s, want %s", last, want)
	}
	// Group-readable for the PC backup pull, never world-readable (it holds secrets).
	fi, err := os.Stat(last)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o640 {
		t.Errorf("snapshot mode = %v, want 0640", fi.Mode().Perm())
	}
	got, _ := filepath.Glob(filepath.Join(dir, "quoker-*.db"))
	if len(got) != backup.Keep {
		t.Fatalf("kept %d files, want %d: %v", len(got), backup.Keep, got)
	}
	if got[0] != filepath.Join(dir, "quoker-20260101-030000.db") {
		t.Errorf("oldest kept = %s", got[0])
	}
	for _, p := range []string{stale} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s should be removed", p)
		}
	}
	if _, err := os.Stat(other); err != nil {
		t.Errorf("unrelated file touched: %v", err)
	}

	// The copy opens as a normal database with the data.
	cp, err := sql.Open("sqlite", "file:"+last+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cp.Close() }()
	var n int
	if err := cp.QueryRowContext(ctx, `SELECT count(*) FROM workers`).Scan(&n); err != nil || n != 1 {
		t.Errorf("copy workers = %d, %v", n, err)
	}
}

func TestRunCreatesDirAndFailsOnBadDir(t *testing.T) {
	st := openStore(t)
	file := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := backup.New(st.DB(), filepath.Join(file, "sub"), nil).Run(context.Background()); err == nil {
		t.Error("expected error when the directory cannot be created")
	}
}

func TestNextRun(t *testing.T) {
	msk, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		t.Skip("no tzdata")
	}
	tests := []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{"before 04:00", time.Date(2026, 3, 1, 2, 0, 0, 0, msk), time.Date(2026, 3, 1, 4, 0, 0, 0, msk)},
		{"exactly 04:00 goes to next day", time.Date(2026, 3, 1, 4, 0, 0, 0, msk), time.Date(2026, 3, 2, 4, 0, 0, 0, msk)},
		{"after 04:00", time.Date(2026, 3, 1, 23, 0, 0, 0, msk), time.Date(2026, 3, 2, 4, 0, 0, 0, msk)},
		{"given in UTC", time.Date(2026, 3, 1, 0, 30, 0, 0, time.UTC), time.Date(2026, 3, 1, 4, 0, 0, 0, msk)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := backup.NextRun(tt.now, msk); !got.Equal(tt.want) {
				t.Errorf("NextRun = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestSchedulerRunsDaily(t *testing.T) {
	msk := time.FixedZone("MSK", 3*3600)
	st := openStore(t)
	dir := t.TempDir()
	clock := time.Date(2026, 3, 1, 12, 0, 0, 0, msk)
	var waits []time.Duration
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := &backup.Scheduler{
		Backup: backup.New(st.DB(), dir, func() time.Time { return clock }),
		Loc:    msk,
		Log:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		Now:    func() time.Time { return clock },
		Sleep: func(ctx context.Context, d time.Duration) error {
			waits = append(waits, d)
			if len(waits) > 3 {
				cancel()
				return ctx.Err()
			}
			clock = clock.Add(d) // fake time jumps to the wake-up moment
			return nil
		},
	}
	s.Run(ctx)
	want := []time.Duration{16 * time.Hour, 24 * time.Hour, 24 * time.Hour, 24 * time.Hour}
	if len(waits) != len(want) {
		t.Fatalf("waits = %v", waits)
	}
	for i := range want {
		if waits[i] != want[i] {
			t.Errorf("wait %d = %v, want %v", i, waits[i], want[i])
		}
	}
	files, _ := filepath.Glob(filepath.Join(dir, "quoker-*.db"))
	if len(files) != 3 {
		t.Errorf("backups = %v, want 3", files)
	}
}
