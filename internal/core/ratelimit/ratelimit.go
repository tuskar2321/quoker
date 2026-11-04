// Package ratelimit implements the request limits of docs/03-public-api.md.
// Counters live in the rate_limits table (docs/05-data-model.md), so a
// restart or deploy does not reset them.
package ratelimit

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/tuskar2321/quoker/internal/core/store"
)

// Limiter allows limit events per key per fixed window. Windows are aligned
// to multiples of their length (calendar hours for time.Hour).
type Limiter struct {
	st     *store.Store
	bucket string
	limit  int
	window time.Duration
	now    func() time.Time
}

// New returns a Limiter for bucket; now defaults to time.Now.
func New(st *store.Store, bucket string, limit int, window time.Duration, now func() time.Time) *Limiter {
	if now == nil {
		now = time.Now
	}
	return &Limiter{st: st, bucket: bucket, limit: limit, window: window, now: now}
}

// Allow records an event for userID (0 for anonymous) and scope (e.g. an IP;
// empty for per-user limits). When the limit is exhausted it returns false
// and the time until the window resets.
func (l *Limiter) Allow(ctx context.Context, userID int64, scope string) (bool, time.Duration, error) {
	now := l.now()
	start := now.Truncate(l.window)
	bucket := l.bucket
	if scope != "" {
		bucket += ":" + scope
	}
	// One statement: a row at the limit is not updated and returns nothing,
	// so concurrent requests cannot slip past it.
	var count int
	err := l.st.DB().QueryRowContext(ctx, `
		INSERT INTO rate_limits (user_id, bucket, window_start, count) VALUES (?, ?, ?, 1)
		ON CONFLICT (user_id, bucket, window_start) DO UPDATE SET count = count + 1 WHERE count < ?
		RETURNING count`,
		userID, bucket, start.UnixMilli(), l.limit).Scan(&count)
	if errors.Is(err, sql.ErrNoRows) {
		return false, start.Add(l.window).Sub(now), nil
	}
	if err != nil {
		return false, 0, fmt.Errorf("rate limit %s: %w", bucket, err)
	}
	return true, 0, nil
}

// Purge deletes counters of windows that ended before cutoff.
func Purge(ctx context.Context, st *store.Store, cutoff time.Time) error {
	_, err := st.DB().ExecContext(ctx, `DELETE FROM rate_limits WHERE window_start < ?`, cutoff.UnixMilli())
	return err
}

// RunPurge deletes counters older than keep every interval until ctx ends.
func RunPurge(ctx context.Context, st *store.Store, interval, keep time.Duration, onErr func(error)) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		if err := Purge(ctx, st, time.Now().Add(-keep)); err != nil && ctx.Err() == nil && onErr != nil {
			onErr(err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
