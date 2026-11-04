package ratelimit_test

import (
	"context"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tuskar2321/quoker/internal/core/ratelimit"
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

func TestLimiter(t *testing.T) {
	st := openStore(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 3, 10, 59, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	l := ratelimit.New(st, "send", 2, time.Hour, clock)

	steps := []struct {
		user      int64
		scope     string
		advance   time.Duration
		wantOK    bool
		wantRetry time.Duration
	}{
		{user: 1, wantOK: true},
		{user: 1, wantOK: true},
		{user: 1, wantOK: false, wantRetry: time.Minute},
		{user: 2, wantOK: true},                       // users are independent
		{user: 0, scope: "203.0.113.7", wantOK: true}, // anonymous, keyed by scope
		{user: 1, advance: time.Minute, wantOK: true}, // 11:00, a new calendar hour
	}
	for i, s := range steps {
		now = now.Add(s.advance)
		ok, retry, err := l.Allow(ctx, s.user, s.scope)
		if err != nil || ok != s.wantOK || retry != s.wantRetry {
			t.Fatalf("step %d: Allow = %v, %v, %v; want %v, %v", i, ok, retry, err, s.wantOK, s.wantRetry)
		}
	}

	// A restart builds a new limiter; the count lives in the database.
	restarted := ratelimit.New(st, "send", 2, time.Hour, clock)
	_, _, _ = restarted.Allow(ctx, 3, "")
	_, _, _ = restarted.Allow(ctx, 3, "")
	again := ratelimit.New(st, "send", 2, time.Hour, clock)
	if ok, _, _ := again.Allow(ctx, 3, ""); ok {
		t.Fatal("counter reset by a restart")
	}

	if err := ratelimit.Purge(ctx, st, now.Truncate(time.Hour)); err != nil {
		t.Fatal(err)
	}
	var left int
	_ = st.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM rate_limits WHERE window_start < ?`, now.Truncate(time.Hour).UnixMilli()).Scan(&left)
	if left != 0 {
		t.Fatalf("old windows left: %d", left)
	}
}

// TestLimiterConcurrent: parallel requests never get past the limit.
func TestLimiterConcurrent(t *testing.T) {
	l := ratelimit.New(openStore(t), "resolve", 5, time.Hour, nil)
	var allowed atomic.Int32
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if ok, _, err := l.Allow(context.Background(), 1, ""); err == nil && ok {
				allowed.Add(1)
			}
		}()
	}
	wg.Wait()
	if got := allowed.Load(); got != 5 {
		t.Fatalf("allowed = %d, want 5", got)
	}
}
