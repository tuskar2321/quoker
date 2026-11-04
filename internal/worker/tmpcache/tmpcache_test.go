package tmpcache_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tuskar2321/quoker/internal/worker/tmpcache"
)

func newCache(t *testing.T, maxBytes int64) *tmpcache.Cache {
	t.Helper()
	c, err := tmpcache.New(filepath.Join(t.TempDir(), "files"), maxBytes)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func put(t *testing.T, c *tmpcache.Cache, key string, size int) *tmpcache.Handle {
	t.Helper()
	dir, err := c.StagingDir()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	src := filepath.Join(dir, "v.mp4")
	if err := os.WriteFile(src, make([]byte, size), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := c.Put(key, src, []byte(key))
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func TestLRUEviction(t *testing.T) {
	c := newCache(t, 250)
	a := put(t, c, "tiktok:1/phone", 100)
	a.Release()
	b := put(t, c, "tiktok:2/phone", 100)
	b.Release()
	if h, ok := c.Get("tiktok:1/phone"); ok { // a becomes most recent
		h.Release()
	} else {
		t.Fatal("a missing")
	}
	put(t, c, "tiktok:3/phone", 100).Release()

	if _, ok := c.Get("tiktok:2/phone"); ok {
		t.Fatal("least recently used entry not evicted")
	}
	if exists(b.Path) {
		t.Fatal("evicted file still on disk")
	}
	if c.Size() != 200 {
		t.Fatalf("size = %d, want 200", c.Size())
	}
}

func TestPinnedNotEvicted(t *testing.T) {
	c := newCache(t, 150)
	a := put(t, c, "a", 100) // kept pinned
	put(t, c, "b", 100).Release()

	if !exists(a.Path) {
		t.Fatal("pinned file evicted")
	}
	if _, ok := c.Get("b"); ok {
		t.Fatal("unpinned entry should have been evicted instead")
	}
	a.Release()
	a.Release() // idempotent
}

func TestReplaceWhilePinned(t *testing.T) {
	c := newCache(t, 1000)
	old := put(t, c, "k", 10)
	fresh := put(t, c, "k", 20)
	defer fresh.Release()

	if b, err := os.ReadFile(old.Path); err != nil || len(b) != 10 {
		t.Fatalf("pinned old file damaged: %v len=%d", err, len(b))
	}
	old.Release()
	if exists(old.Path) {
		t.Fatal("old file not removed after release")
	}
	if h, ok := c.Get("k"); !ok || h.Size != 20 || string(h.Meta) != "k" {
		t.Fatalf("Get after replace = %+v %v", h, ok)
	}
}

func TestNewWipesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "files")
	c, _ := tmpcache.New(dir, 100)
	h := put(t, c, "k", 10)
	if _, err := tmpcache.New(dir, 100); err != nil {
		t.Fatal(err)
	}
	if exists(h.Path) {
		t.Fatal("leftover file survived restart")
	}
}
