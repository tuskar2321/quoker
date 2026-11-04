package ratelimit

import (
	"testing"
	"time"
)

func TestMemory(t *testing.T) {
	now := time.Date(2026, 11, 3, 10, 59, 0, 0, time.UTC)
	m := NewMemory(2, time.Hour, func() time.Time { return now })

	steps := []struct {
		key       string
		advance   time.Duration
		wantOK    bool
		wantRetry time.Duration
	}{
		{key: "a", wantOK: true},
		{key: "a", wantOK: true},
		{key: "a", wantOK: false, wantRetry: time.Minute},
		{key: "b", wantOK: true},                       // keys are independent
		{key: "a", advance: time.Minute, wantOK: true}, // 11:00, new calendar hour
	}
	for i, s := range steps {
		now = now.Add(s.advance)
		ok, retry := m.Allow(s.key)
		if ok != s.wantOK || retry != s.wantRetry {
			t.Fatalf("step %d: Allow(%s) = %v, %v; want %v, %v", i, s.key, ok, retry, s.wantOK, s.wantRetry)
		}
	}
}
