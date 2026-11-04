package releases_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/tuskar2321/quoker/internal/core/releases"
	"github.com/tuskar2321/quoker/internal/core/store"
	"github.com/tuskar2321/quoker/internal/shared/errs"
)

func setup(t *testing.T) (*releases.Store, string) {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "q.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	dir := filepath.Join(t.TempDir(), "releases")
	return releases.New(st, dir, nil), dir
}

func apk(t *testing.T, dir, name, body string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("PK\x03\x04"+body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestAddAndLatest(t *testing.T) {
	s, dir := setup(t)
	ctx := context.Background()
	src := apk(t, t.TempDir(), "app-release.apk", "v1")
	r, err := s.Add(ctx, 10, "1.0.0", src, "  Первая версия \n")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("PK\x03\x04v1"))
	if r.FileName != "quoker-1.0.0.apk" || r.SHA256 != hex.EncodeToString(sum[:]) || r.Changelog != "Первая версия" {
		t.Fatalf("release = %+v", r)
	}
	b, err := os.ReadFile(filepath.Join(dir, "quoker-1.0.0.apk")) //nolint:gosec // test temp dir
	if err != nil || string(b) != "PK\x03\x04v1" {
		t.Fatalf("copied apk = %q, %v", b, err)
	}

	// CI puts the file in place first; Add then only hashes it.
	inPlace := apk(t, dir, "quoker-1.1.0.apk", "v2")
	if _, err := s.Add(ctx, 11, "1.1.0", inPlace, ""); err != nil {
		t.Fatal(err)
	}
	latest, err := s.Latest(ctx)
	if err != nil || latest.VersionCode != 11 || latest.VersionName != "1.1.0" {
		t.Fatalf("latest = %+v, %v", latest, err)
	}
	f, rel, err := s.Open(ctx, "quoker-1.1.0.apk")
	if err != nil || rel.VersionCode != 11 {
		t.Fatalf("open = %+v, %v", rel, err)
	}
	_ = f.Close()
}

func TestAddRejects(t *testing.T) {
	s, _ := setup(t)
	ctx := context.Background()
	good := apk(t, t.TempDir(), "a.apk", "x")
	notZip := filepath.Join(t.TempDir(), "b.apk")
	_ = os.WriteFile(notZip, []byte("<html>"), 0o600)
	if _, err := s.Add(ctx, 1, "1.0.0", good, ""); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		code int64
		ver  string
		src  string
	}{
		{"duplicate code", 1, "1.0.1", good},
		{"duplicate name", 2, "1.0.0", good},
		{"zero code", 0, "1.0.1", good},
		{"path in name", 2, "../../etc", good},
		{"not an apk", 2, "1.0.2", notZip},
		{"missing file", 2, "1.0.2", "/nope.apk"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := s.Add(ctx, tt.code, tt.ver, tt.src, ""); !errs.Is(err, errs.InvalidRequest) {
				t.Fatalf("err = %v, want invalid_request", err)
			}
		})
	}
}

func TestOpenOnlyRegistered(t *testing.T) {
	s, dir := setup(t)
	ctx := context.Background()
	if _, err := s.Latest(ctx); !errs.Is(err, errs.NotFound) {
		t.Fatalf("latest with none: %v", err)
	}
	apk(t, dir, "quoker-9.9.9.apk", "unregistered")
	for _, name := range []string{"quoker-9.9.9.apk", "../q.db", "quoker-1.0.0.apk"} {
		if _, _, err := s.Open(ctx, name); !errs.Is(err, errs.NotFound) {
			t.Fatalf("Open(%q) = %v", name, err)
		}
	}
}

func TestRejectedAddLeavesNoFile(t *testing.T) {
	s, dir := setup(t)
	ctx := context.Background()
	src := apk(t, t.TempDir(), "a.apk", "x")
	if _, err := s.Add(ctx, 1, "1.0.0", src, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add(ctx, 1, "1.0.1", src, ""); err == nil {
		t.Fatal("duplicate accepted")
	}
	if _, err := os.Stat(filepath.Join(dir, "quoker-1.0.1.apk")); !os.IsNotExist(err) {
		t.Fatalf("rejected release left its file: %v", err)
	}
}
