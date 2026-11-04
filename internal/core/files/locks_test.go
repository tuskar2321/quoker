package files

import (
	"sync"
	"testing"
)

// TestUploadLocksAreReleased: per-video locks do not pile up (REVIEW L17).
func TestUploadLocksAreReleased(t *testing.T) {
	s := &Store{uploads: map[string]*uploadLock{}, pins: map[string]int{}}
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			unlock := s.lockUpload("tiktok:1")
			unlock()
		}()
	}
	wg.Wait()
	if len(s.uploads) != 0 {
		t.Fatalf("locks left: %d", len(s.uploads))
	}
}
