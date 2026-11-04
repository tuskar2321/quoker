// Package files keeps the core's cache of video files and thumbnails: uploads
// from workers, Range serving, LRU eviction and periodic cleanup.
// See docs/05-data-model.md ("Файлы на диске ядра").
package files

import (
	"bytes"
	"context"
	"crypto/sha1" //nolint:gosec // file naming only, per docs/05-data-model.md
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"github.com/tuskar2321/quoker/internal/core/store"
	"github.com/tuskar2321/quoker/internal/shared/errs"
)

const (
	// MaxFileBytes is the phone download limit (docs/11-nfr-security.md).
	MaxFileBytes = 500 << 20
	// MaxThumbBytes is the cover limit (docs/07-downloading.md).
	MaxThumbBytes = 512 << 10
)

// ErrOffset means an upload chunk does not start where the stored part ends.
var ErrOffset = errors.New("upload offset mismatch")

var validVideoID = regexp.MustCompile(`^(tiktok|instagram):[A-Za-z0-9_-]{1,64}$`)

// ValidVideoID reports whether id has the "<platform>:<native_id>" form.
func ValidVideoID(id string) bool { return validVideoID.MatchString(id) }

// Store is the on-disk cache.
type Store struct {
	st       *store.Store
	root     string
	maxBytes int64
	now      func() time.Time
	log      *slog.Logger

	mu      sync.Mutex
	pins    map[string]int         // video id → active readers
	uploads map[string]*uploadLock // video id → upload lock
}

// New creates the cache directories under root.
func New(st *store.Store, root string, maxBytes int64, now func() time.Time, log *slog.Logger) (*Store, error) {
	for _, d := range []string{"files", "thumbs", "partial"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o700); err != nil {
			return nil, fmt.Errorf("create cache dir: %w", err)
		}
	}
	if now == nil {
		now = time.Now
	}
	if log == nil {
		log = slog.Default()
	}
	return &Store{
		st: st, root: root, maxBytes: maxBytes, now: now, log: log,
		pins: map[string]int{}, uploads: map[string]*uploadLock{},
	}, nil
}

func diskName(id string) string {
	sum := sha1.Sum([]byte(id)) //nolint:gosec // naming, not security
	return hex.EncodeToString(sum[:])
}

func (s *Store) filePath(id string) string {
	return filepath.Join(s.root, "files", diskName(id)+".mp4")
}

func (s *Store) thumbPath(id string) string {
	return filepath.Join(s.root, "thumbs", diskName(id)+".jpg")
}

func (s *Store) partialPath(id string) string {
	return filepath.Join(s.root, "partial", diskName(id)+".part")
}

// HasFile reports whether the complete file for id is on disk.
func (s *Store) HasFile(id string) bool {
	_, err := os.Stat(s.filePath(id))
	return err == nil
}

// pin protects id from eviction and cleanup until the returned func runs.
func (s *Store) pin(id string) func() {
	s.mu.Lock()
	s.pins[id]++
	s.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			if s.pins[id]--; s.pins[id] == 0 {
				delete(s.pins, id)
			}
		})
	}
}

func (s *Store) pinned(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pins[id] > 0
}

// OpenFile opens the complete file for serving; release unpins it.
func (s *Store) OpenFile(id string) (f *os.File, release func(), err error) {
	release = s.pin(id)
	f, err = os.Open(s.filePath(id))
	if err != nil {
		release()
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil, errs.New(errs.FileNotReady, "file not in cache")
		}
		return nil, nil, fmt.Errorf("open cached file: %w", err)
	}
	return f, release, nil
}

// OpenThumb opens the cover for id.
func (s *Store) OpenThumb(id string) (*os.File, error) {
	f, err := os.Open(s.thumbPath(id))
	if errors.Is(err, os.ErrNotExist) {
		return nil, errs.New(errs.NotFound, "no thumbnail")
	}
	if err != nil {
		return nil, fmt.Errorf("open thumbnail: %w", err)
	}
	return f, nil
}

// PutThumb stores a JPEG cover. The video row may not exist yet: workers
// upload the cover before reporting the resolve result.
func (s *Store) PutThumb(ctx context.Context, id string, body io.Reader) error {
	b, err := io.ReadAll(io.LimitReader(body, MaxThumbBytes+1))
	if err != nil {
		return fmt.Errorf("read thumbnail: %w", err)
	}
	if len(b) > MaxThumbBytes {
		return errs.New(errs.TooLarge, "thumbnail too large")
	}
	if !bytes.HasPrefix(b, []byte{0xFF, 0xD8, 0xFF}) {
		return errs.New(errs.InvalidRequest, "thumbnail is not a JPEG")
	}
	if err := writeAtomic(s.thumbPath(id), b); err != nil {
		return err
	}
	_, err = s.st.DB().ExecContext(ctx, `UPDATE videos SET has_thumbnail = 1 WHERE id = ?`, id)
	return err
}

// Received returns how many bytes of id the core holds: the complete file's
// size, or the partial upload's. ok is false when there is nothing.
func (s *Store) Received(id string) (n int64, ok bool) {
	for _, p := range []string{s.filePath(id), s.partialPath(id)} {
		if st, err := os.Stat(p); err == nil {
			return st.Size(), true
		}
	}
	return 0, false
}

// Append writes an upload chunk starting at from of a total-byte file. The
// part becomes the cached file once all bytes have arrived.
func (s *Store) Append(id string, from, total int64, body io.Reader) (complete bool, err error) {
	if total <= 0 || from < 0 || from >= total {
		return false, errs.New(errs.InvalidRequest, "bad range")
	}
	if total > MaxFileBytes || total > s.maxBytes {
		return false, errs.New(errs.TooLarge, fmt.Sprintf("file of %d bytes exceeds the limit", total))
	}
	unlock := s.lockUpload(id)
	defer unlock()

	part := s.partialPath(id)
	var cur int64
	if st, err := os.Stat(part); err == nil {
		cur = st.Size()
	}
	if from != cur {
		return false, ErrOffset
	}
	f, err := os.OpenFile(part, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600) //nolint:gosec // path derived from a hash
	if err != nil {
		return false, fmt.Errorf("open partial: %w", err)
	}
	n, copyErr := io.Copy(f, io.LimitReader(body, total-from))
	if err := f.Close(); err != nil && copyErr == nil {
		copyErr = err
	}
	if copyErr != nil {
		return false, fmt.Errorf("write partial: %w", copyErr) // the bytes written stay for resuming
	}
	if from+n < total {
		return false, nil
	}
	if err := os.Rename(part, s.filePath(id)); err != nil {
		return false, fmt.Errorf("publish file: %w", err)
	}
	return true, nil
}

type uploadLock struct {
	sync.Mutex
	users int // holders and waiters; the entry goes when it drops to zero
}

func (s *Store) lockUpload(id string) func() {
	s.mu.Lock()
	l, ok := s.uploads[id]
	if !ok {
		l = &uploadLock{}
		s.uploads[id] = l
	}
	l.users++
	s.mu.Unlock()
	l.Lock()
	return func() {
		l.Unlock()
		s.mu.Lock()
		if l.users--; l.users == 0 {
			delete(s.uploads, id)
		}
		s.mu.Unlock()
	}
}

// Verify checks the complete file against the size and SHA-256 the worker
// reported and removes it on mismatch.
func (s *Store) Verify(id string, size int64, sha256Hex string) error {
	p := s.filePath(id)
	f, err := os.Open(p) //nolint:gosec // path derived from a hash
	if err != nil {
		return fmt.Errorf("open uploaded file: %w", err)
	}
	h := sha256.New()
	n, err := io.Copy(h, f)
	_ = f.Close()
	if err != nil {
		return fmt.Errorf("hash uploaded file: %w", err)
	}
	if n != size || hex.EncodeToString(h.Sum(nil)) != sha256Hex {
		_ = os.Remove(p)
		return fmt.Errorf("uploaded file mismatch: %d bytes, want %d", n, size)
	}
	return nil
}

// Discard removes any file and partial upload of id.
func (s *Store) Discard(id string) {
	_ = os.Remove(s.filePath(id))
	_ = os.Remove(s.partialPath(id))
}

func writeAtomic(path string, b []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("publish %s: %w", filepath.Base(path), err)
	}
	return nil
}
