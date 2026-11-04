package store

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func openTemp(t *testing.T) (*Store, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	p := filepath.Join(t.TempDir(), "quoker.db")
	s, err := Open(ctx, p)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, p
}

func TestOpenMigratesAndIsIdempotent(t *testing.T) {
	ctx := context.Background()
	s, p := openTemp(t)

	if err := s.Ready(ctx); err != nil {
		t.Fatalf("Ready: %v", err)
	}
	var version int
	if err := s.DB().QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 2 {
		t.Fatalf("user_version = %d, want 2", version)
	}

	tables := []string{"users", "invites", "sessions", "link_tokens", "targets", "videos",
		"video_aliases", "tasks", "sends", "history", "history_videos", "workers",
		"bot_updates", "settings", "app_releases", "rate_limits"}
	for _, tbl := range tables {
		var n int
		err := s.DB().QueryRowContext(ctx,
			"SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?", tbl).Scan(&n)
		if err != nil || n != 1 {
			t.Errorf("table %s: n=%d err=%v", tbl, n, err)
		}
	}

	// Reopening must not re-apply migrations.
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s2, err := Open(ctx, p)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	_ = s2.Close()
}

func TestPragmas(t *testing.T) {
	ctx := context.Background()
	s, _ := openTemp(t)
	tests := []struct {
		pragma string
		want   string
	}{
		{"journal_mode", "wal"},
		{"foreign_keys", "1"},
		{"busy_timeout", "5000"},
	}
	for _, tt := range tests {
		var got string
		if err := s.DB().QueryRowContext(ctx, "PRAGMA "+tt.pragma).Scan(&got); err != nil {
			t.Fatalf("%s: %v", tt.pragma, err)
		}
		if got != tt.want {
			t.Errorf("%s = %q, want %q", tt.pragma, got, tt.want)
		}
	}
}

func TestForeignKeysEnforced(t *testing.T) {
	ctx := context.Background()
	s, _ := openTemp(t)
	_, err := s.DB().ExecContext(ctx,
		`INSERT INTO sessions (user_id, token_hash, device_id, device_name, created_at, last_seen_at)
		 VALUES (999, x'00', 'd', 'n', 0, 0)`)
	if err == nil {
		t.Fatal("insert with missing user succeeded, want foreign key error")
	}
}

func TestLoadMigrations(t *testing.T) {
	tests := []struct {
		name    string
		files   fstest.MapFS
		wantErr string
		wantN   int
	}{
		{
			name:  "ok sorted",
			files: fstest.MapFS{"0002_b.sql": {Data: []byte("b")}, "0001_a.sql": {Data: []byte("a")}, "README": {}},
			wantN: 2,
		},
		{
			name:    "gap",
			files:   fstest.MapFS{"0001_a.sql": {}, "0003_c.sql": {}},
			wantErr: "expected version 2",
		},
		{
			name:    "bad prefix",
			files:   fstest.MapFS{"init.sql": {}},
			wantErr: "NNNN_description",
		},
		{
			name:    "duplicate",
			files:   fstest.MapFS{"0001_a.sql": {}, "0001_b.sql": {}},
			wantErr: "expected version 2",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ms, err := loadMigrations(tt.files)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(ms) != tt.wantN {
				t.Fatalf("len = %d, want %d", len(ms), tt.wantN)
			}
			for i, m := range ms {
				if m.version != i+1 {
					t.Errorf("ms[%d].version = %d", i, m.version)
				}
			}
		})
	}
}

func TestMigrateRejectsNewerSchema(t *testing.T) {
	ctx := context.Background()
	s, _ := openTemp(t)
	if _, err := s.DB().ExecContext(ctx, "PRAGMA user_version = 99"); err != nil {
		t.Fatal(err)
	}
	err := migrate(ctx, s.DB(), fstest.MapFS{"0001_a.sql": {Data: []byte("SELECT 1;")}})
	if err == nil || !strings.Contains(err.Error(), "newer") {
		t.Fatalf("err = %v, want newer-schema error", err)
	}
}
