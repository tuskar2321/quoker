// Package tmpcache keeps recently downloaded videos on the worker so that a
// send after a download does not fetch the video again (docs/07-downloading.md).
package tmpcache

import (
	"container/list"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Cache is a size-bounded LRU of files. Files handed out are pinned until
// released and are never evicted while in use.
type Cache struct {
	dir string
	max int64

	mu    sync.Mutex
	items map[string]*item
	lru   *list.List // front is the most recently used
	size  int64
	seq   uint64 // every stored version gets its own file
}

type item struct {
	key   string
	path  string
	size  int64
	meta  []byte
	refs  int
	stale bool // replaced or evicted while pinned; delete on last release
	elem  *list.Element
}

// New wipes dir and returns an empty cache limited to maxBytes.
func New(dir string, maxBytes int64) (*Cache, error) {
	if err := os.RemoveAll(dir); err != nil {
		return nil, fmt.Errorf("reset cache dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "staging"), 0o700); err != nil {
		return nil, fmt.Errorf("create cache dir: %w", err)
	}
	return &Cache{dir: dir, max: maxBytes, items: make(map[string]*item), lru: list.New()}, nil
}

// StagingDir creates a scratch directory on the cache's filesystem, so Put can
// move files in with a rename. The caller removes it.
func (c *Cache) StagingDir() (string, error) {
	d, err := os.MkdirTemp(filepath.Join(c.dir, "staging"), "task-")
	if err != nil {
		return "", fmt.Errorf("staging dir: %w", err)
	}
	return d, nil
}

// Size returns the bytes currently held.
func (c *Cache) Size() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.size
}

// Handle is a pinned cache file.
type Handle struct {
	Path string
	Size int64
	Meta []byte // opaque data stored with the file
	once sync.Once
	rel  func()
}

// Release unpins the file. It is safe to call more than once.
func (h *Handle) Release() { h.once.Do(h.rel) }

// Get returns a pinned handle for key.
func (c *Cache) Get(key string) (*Handle, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	it, ok := c.items[key]
	if !ok {
		return nil, false
	}
	c.lru.MoveToFront(it.elem)
	return c.pin(it), true
}

// Put moves src into the cache under key with opaque meta and returns it
// pinned. Older entries are evicted until the cache fits its limit.
func (c *Cache) Put(key, src string, meta []byte) (*Handle, error) {
	st, err := os.Stat(src)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", src, err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seq++
	dst := filepath.Join(c.dir, fileName(key, c.seq))
	if err := os.Rename(src, dst); err != nil {
		return nil, fmt.Errorf("move into cache: %w", err)
	}
	if old, ok := c.items[key]; ok {
		c.drop(old) // a pinned old version stays readable until released
	}
	it := &item{key: key, path: dst, size: st.Size(), meta: meta}
	it.elem = c.lru.PushFront(it)
	c.items[key] = it
	c.size += it.size
	h := c.pin(it)
	c.evict()
	return h, nil
}

func (c *Cache) pin(it *item) *Handle {
	it.refs++
	return &Handle{Path: it.path, Size: it.size, Meta: it.meta, rel: func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		it.refs--
		if it.refs > 0 {
			return
		}
		if it.stale {
			_ = os.Remove(it.path)
		}
		c.evict()
	}}
}

// drop removes it from the index; the file goes now or on its last release.
func (c *Cache) drop(it *item) {
	delete(c.items, it.key)
	c.lru.Remove(it.elem)
	c.size -= it.size
	it.stale = true
	if it.refs == 0 {
		_ = os.Remove(it.path)
	}
}

func (c *Cache) evict() {
	for e := c.lru.Back(); e != nil && c.size > c.max; {
		prev := e.Prev()
		if it := e.Value.(*item); it.refs == 0 {
			c.drop(it)
		}
		e = prev
	}
}

// fileName maps an arbitrary key to a safe, collision-resistant file name.
func fileName(key string, seq uint64) string {
	sum := sha256.Sum256([]byte(key))
	safe := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, key)
	if len(safe) > 64 {
		safe = safe[:64]
	}
	return fmt.Sprintf("%s-%s-%d.mp4", safe, hex.EncodeToString(sum[:4]), seq)
}
