// Package releases keeps the APK builds the core distributes
// (docs/01-product.md F-24, F-25; docs/10-deploy.md release-android).
package releases

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/tuskar2321/quoker/internal/core/store"
	"github.com/tuskar2321/quoker/internal/shared/errs"
)

// maxChangelog keeps the update dialog readable.
const maxChangelog = 4000

var versionNamePattern = regexp.MustCompile(`^[0-9]+(\.[0-9]+){0,3}([-+][0-9A-Za-z.]+)?$`)

// Release is a published APK.
type Release struct {
	VersionCode int64
	VersionName string
	FileName    string // quoker-<version_name>.apk inside the releases dir
	SHA256      string
	Changelog   string
	PublishedAt time.Time
}

// Store manages releases on disk and in app_releases.
type Store struct {
	st  *store.Store
	dir string
	now func() time.Time
}

// New returns a Store for dir.
func New(st *store.Store, dir string, now func() time.Time) *Store {
	if now == nil {
		now = time.Now
	}
	return &Store{st: st, dir: dir, now: now}
}

// FileName is the published name of a version's APK.
func FileName(versionName string) string { return "quoker-" + versionName + ".apk" }

// Add registers an APK. src may already sit in the releases directory under
// its published name (CI copies it there); otherwise it is copied in.
func (s *Store) Add(ctx context.Context, versionCode int64, versionName, src, changelog string) (Release, error) {
	switch {
	case versionCode <= 0:
		return Release{}, errs.New(errs.InvalidRequest, "version_code must be positive")
	case !versionNamePattern.MatchString(versionName):
		return Release{}, errs.New(errs.InvalidRequest, fmt.Sprintf("version_name %q is not like 1.4.0", versionName))
	case len(changelog) > maxChangelog:
		return Release{}, errs.New(errs.InvalidRequest, "changelog is too long")
	}
	var taken int
	if err := s.st.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM app_releases WHERE version_code = ? OR version_name = ?`, versionCode, versionName).Scan(&taken); err != nil {
		return Release{}, fmt.Errorf("check release: %w", err)
	}
	if taken > 0 {
		return Release{}, errs.New(errs.InvalidRequest, fmt.Sprintf("version %d or %s is already published", versionCode, versionName))
	}
	if err := os.MkdirAll(s.dir, 0o750); err != nil {
		return Release{}, fmt.Errorf("releases dir: %w", err)
	}
	name := FileName(versionName)
	dst := filepath.Join(s.dir, name)
	sum, copied, err := s.install(src, dst)
	if err != nil {
		return Release{}, err
	}
	r := Release{
		VersionCode: versionCode, VersionName: versionName, FileName: name,
		SHA256: sum, Changelog: strings.TrimSpace(changelog), PublishedAt: s.now().UTC(),
	}
	_, err = s.st.DB().ExecContext(ctx,
		`INSERT INTO app_releases (version_code, version_name, apk_path, sha256, changelog, published_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		r.VersionCode, r.VersionName, r.FileName, r.SHA256, r.Changelog, r.PublishedAt.UnixMilli())
	if err != nil {
		if copied {
			_ = os.Remove(dst)
		}
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return Release{}, errs.New(errs.InvalidRequest, fmt.Sprintf("version_code %d is already published", versionCode))
		}
		return Release{}, fmt.Errorf("insert release: %w", err)
	}
	return r, nil
}

// install checks that src is an APK, places it at dst and returns its SHA-256
// and whether it had to copy the file.
func (s *Store) install(src, dst string) (sum string, copied bool, err error) {
	f, err := os.Open(src) //nolint:gosec // operator-supplied path on the CLI
	if err != nil {
		return "", false, errs.Wrap(errs.InvalidRequest, fmt.Errorf("open apk: %w", err))
	}
	defer func() { _ = f.Close() }()
	magic := make([]byte, 4)
	if _, err := io.ReadFull(f, magic); err != nil || !bytes.Equal(magic, []byte("PK\x03\x04")) {
		return "", false, errs.New(errs.InvalidRequest, "file is not an APK (zip)")
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", false, fmt.Errorf("rewind apk: %w", err)
	}

	same, err := samePath(src, dst)
	if err != nil {
		return "", false, err
	}
	h := sha256.New()
	if same {
		if _, err := io.Copy(h, f); err != nil {
			return "", false, fmt.Errorf("hash apk: %w", err)
		}
		return hex.EncodeToString(h.Sum(nil)), false, nil
	}
	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o640) //nolint:gosec // inside the releases dir
	if err != nil {
		return "", false, fmt.Errorf("create apk: %w", err)
	}
	_, copyErr := io.Copy(io.MultiWriter(out, h), f)
	if err := out.Close(); err != nil && copyErr == nil {
		copyErr = err
	}
	if copyErr != nil {
		_ = os.Remove(tmp)
		return "", false, fmt.Errorf("copy apk: %w", copyErr)
	}
	if err := os.Rename(tmp, dst); err != nil {
		return "", false, fmt.Errorf("publish apk: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil)), true, nil
}

func samePath(a, b string) (bool, error) {
	sa, err := os.Stat(a)
	if err != nil {
		return false, fmt.Errorf("stat apk: %w", err)
	}
	sb, err := os.Stat(b)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("stat release: %w", err)
	}
	return os.SameFile(sa, sb), nil
}

// Latest returns the release with the highest version_code.
func (s *Store) Latest(ctx context.Context) (Release, error) {
	return s.one(ctx, `ORDER BY version_code DESC LIMIT 1`)
}

// ByFileName returns the release published as name.
func (s *Store) ByFileName(ctx context.Context, name string) (Release, error) {
	return s.one(ctx, `WHERE apk_path = ?`, name)
}

func (s *Store) one(ctx context.Context, where string, args ...any) (Release, error) {
	var (
		r  Release
		ms int64
	)
	err := s.st.DB().QueryRowContext(ctx,
		`SELECT version_code, version_name, apk_path, sha256, changelog, published_at FROM app_releases `+where, args...).
		Scan(&r.VersionCode, &r.VersionName, &r.FileName, &r.SHA256, &r.Changelog, &ms)
	if errors.Is(err, sql.ErrNoRows) {
		return Release{}, errs.New(errs.NotFound, "no release")
	}
	if err != nil {
		return Release{}, fmt.Errorf("get release: %w", err)
	}
	r.PublishedAt = time.UnixMilli(ms).UTC()
	return r, nil
}

// Open opens a registered release's APK. Only names recorded in the table
// are served, so the request path never reaches the filesystem directly.
func (s *Store) Open(ctx context.Context, name string) (*os.File, Release, error) {
	r, err := s.ByFileName(ctx, name)
	if err != nil {
		return nil, Release{}, err
	}
	f, err := os.Open(filepath.Join(s.dir, filepath.Base(r.FileName)))
	if errors.Is(err, os.ErrNotExist) {
		return nil, Release{}, errs.New(errs.NotFound, "apk file is missing")
	}
	if err != nil {
		return nil, Release{}, fmt.Errorf("open apk: %w", err)
	}
	return f, r, nil
}
