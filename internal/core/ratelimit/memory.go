// Package ratelimit implements the request limits of docs/03-public-api.md.
package ratelimit

import (
	"sync"
	"time"
)

// Memory is an in-process fixed-window limiter keyed by an arbitrary string.
// Windows are aligned to multiples of the window length (calendar hours for
// time.Hour). It is used for limits without a user id (registration per IP);
// counters reset on restart, which is acceptable for that purpose.
type Memory struct {
	limit  int
	window time.Duration
	now    func() time.Time

	mu     sync.Mutex
	start  time.Time
	counts map[string]int
}

// NewMemory allows limit events per key per window; now defaults to time.Now.
func NewMemory(limit int, window time.Duration, now func() time.Time) *Memory {
	if now == nil {
		now = time.Now
	}
	return &Memory{limit: limit, window: window, now: now, counts: map[string]int{}}
}

// Allow records an event for key. When the limit is exhausted it returns
// false and the time until the window resets.
func (m *Memory) Allow(key string) (bool, time.Duration) {
	now := m.now()
	start := now.Truncate(m.window)

	m.mu.Lock()
	defer m.mu.Unlock()
	if !start.Equal(m.start) {
		m.start = start
		clear(m.counts)
	}
	if m.counts[key] >= m.limit {
		return false, start.Add(m.window).Sub(now)
	}
	m.counts[key]++
	return true, 0
}
