package queue

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/tuskar2321/quoker/internal/core/store"
	"github.com/tuskar2321/quoker/internal/shared/errs"
)

type fixture struct {
	t   *testing.T
	ctx context.Context
	q   *Store
	now time.Time
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "q.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	f := &fixture{
		t:   t,
		ctx: context.Background(),
		now: time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC),
	}
	f.q = New(st, func() time.Time { return f.now })
	return f
}

func (f *fixture) enqueue(kind Kind, dedupKey string) string {
	f.t.Helper()
	id, err := f.q.Enqueue(f.ctx, EnqueueParams{Kind: kind, Payload: []byte("p"), DedupKey: dedupKey})
	if err != nil && !errors.Is(err, ErrDuplicate) {
		f.t.Fatalf("Enqueue: %v", err)
	}
	return id
}

func TestEnqueueAndLease(t *testing.T) {
	f := newFixture(t)
	id := f.enqueue(KindResolve, "")

	task, err := f.q.Lease(f.ctx, "w1")
	if err != nil {
		t.Fatal(err)
	}
	if task.ID != id || task.Kind != KindResolve || task.Status != StatusLeased {
		t.Fatalf("unexpected task: %+v", task)
	}
	if task.Attempts != 1 {
		t.Fatalf("attempts = %d, want 1", task.Attempts)
	}
	// No more tasks.
	_, err = f.q.Lease(f.ctx, "w1")
	if !errors.Is(err, ErrNoTask) {
		t.Fatalf("second Lease = %v, want ErrNoTask", err)
	}
}

func TestPriority(t *testing.T) {
	f := newFixture(t)
	f.enqueue(KindDownload, "")
	f.enqueue(KindBotCall, "")
	f.enqueue(KindResolve, "")

	orders := []Kind{KindBotCall, KindResolve, KindDownload}
	for _, want := range orders {
		task, err := f.q.Lease(f.ctx, "w1")
		if err != nil {
			t.Fatal(err)
		}
		if task.Kind != want {
			t.Fatalf("got kind %s, want %s", task.Kind, want)
		}
	}
}

func TestDedup(t *testing.T) {
	f := newFixture(t)
	id1, err1 := f.q.Enqueue(f.ctx, EnqueueParams{Kind: KindResolve, Payload: []byte("p"), DedupKey: "k"})
	id2, err2 := f.q.Enqueue(f.ctx, EnqueueParams{Kind: KindResolve, Payload: []byte("p"), DedupKey: "k"})
	if err1 != nil {
		t.Fatalf("first enqueue: %v", err1)
	}
	if !errors.Is(err2, ErrDuplicate) {
		t.Fatalf("second enqueue err = %v, want ErrDuplicate", err2)
	}
	if id1 != id2 {
		t.Fatalf("ids differ: %s vs %s", id1, id2)
	}
}

func TestComplete_success(t *testing.T) {
	f := newFixture(t)
	f.enqueue(KindResolve, "")
	task, _ := f.q.Lease(f.ctx, "w1")

	if err := f.q.Complete(f.ctx, CompleteParams{
		TaskID: task.ID, WorkerID: "w1", Result: []byte("res"),
	}); err != nil {
		t.Fatal(err)
	}
	got, _ := f.q.Get(f.ctx, task.ID)
	if got.Status != StatusDone {
		t.Fatalf("status = %s, want done", got.Status)
	}
}

func TestComplete_retryable(t *testing.T) {
	f := newFixture(t)
	id, _ := f.q.Enqueue(f.ctx, EnqueueParams{Kind: KindResolve, Payload: []byte("p")})
	task, _ := f.q.Lease(f.ctx, "w1")

	if err := f.q.Complete(f.ctx, CompleteParams{
		TaskID: task.ID, WorkerID: "w1",
		ErrCode: "upstream_error", Retryable: true,
	}); err != nil {
		t.Fatal(err)
	}
	got, _ := f.q.Get(f.ctx, id)
	if got.Status != StatusQueued {
		t.Fatalf("after retryable error status = %s, want queued", got.Status)
	}

	// Exhaust retries (maxAttempts=3 for resolve, already 1 attempt).
	for range 2 {
		// Reload to get the current not_before after each retry.
		cur, err := f.q.Get(f.ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		f.now = cur.NotBefore.Add(time.Second)
		task2, err := f.q.Lease(f.ctx, "w1")
		if err != nil {
			t.Fatal(err)
		}
		if err := f.q.Complete(f.ctx, CompleteParams{
			TaskID: task2.ID, WorkerID: "w1",
			ErrCode: "upstream_error", Retryable: true,
		}); err != nil {
			t.Fatal(err)
		}
	}
	final, _ := f.q.Get(f.ctx, id)
	if final.Status != StatusFailed {
		t.Fatalf("after all retries status = %s, want failed", final.Status)
	}
}

func TestComplete_nonRetryable(t *testing.T) {
	f := newFixture(t)
	f.enqueue(KindResolve, "")
	task, _ := f.q.Lease(f.ctx, "w1")

	_ = f.q.Complete(f.ctx, CompleteParams{
		TaskID: task.ID, WorkerID: "w1",
		ErrCode: "private_video", Retryable: false,
	})
	got, _ := f.q.Get(f.ctx, task.ID)
	if got.Status != StatusFailed {
		t.Fatalf("non-retryable error status = %s, want failed", got.Status)
	}
}

func TestCancel(t *testing.T) {
	f := newFixture(t)
	id := f.enqueue(KindResolve, "")
	task, _ := f.q.Lease(f.ctx, "w1")

	if err := f.q.Cancel(f.ctx, task.ID, "w1"); err != nil {
		t.Fatal(err)
	}
	got, _ := f.q.Get(f.ctx, id)
	if got.Status != StatusCancelled {
		t.Fatalf("status = %s, want cancelled", got.Status)
	}
}

func TestRecoverExpiredLeases(t *testing.T) {
	f := newFixture(t)
	// KindResolve has 60s lease and 3 max_attempts.
	f.enqueue(KindResolve, "")
	task, _ := f.q.Lease(f.ctx, "w1")

	// Advance past lease expiry.
	f.now = task.LeaseUntil.Add(time.Second)
	rec, failed, err := f.q.RecoverExpiredLeases(f.ctx)
	if err != nil || rec != 1 || len(failed) != 0 {
		t.Fatalf("recover: rec=%d failed=%v err=%v", rec, failed, err)
	}
	got, _ := f.q.Get(f.ctx, task.ID)
	if got.Status != StatusQueued {
		t.Fatalf("recovered status = %s, want queued", got.Status)
	}
}

func TestGetNotFound(t *testing.T) {
	f := newFixture(t)
	_, err := f.q.Get(f.ctx, "NONEXISTENT")
	if !errs.Is(err, errs.NotFound) {
		t.Fatalf("err = %v, want not_found", err)
	}
}

func TestSetProgress(t *testing.T) {
	f := newFixture(t)
	f.enqueue(KindDownload, "")
	task, _ := f.q.Lease(f.ctx, "w1")

	if err := f.q.SetProgress(f.ctx, task.ID, "w1", 42); err != nil {
		t.Fatal(err)
	}
	// Wrong worker: no-op, no error.
	if err := f.q.SetProgress(f.ctx, task.ID, "other", 50); err != nil {
		t.Fatal(err)
	}
}

func TestNotBefore(t *testing.T) {
	f := newFixture(t)
	future := f.now.Add(time.Minute)
	_, _ = f.q.Enqueue(f.ctx, EnqueueParams{
		Kind:      KindResolve,
		Payload:   []byte("p"),
		NotBefore: future,
	})

	_, err := f.q.Lease(f.ctx, "w1")
	if !errors.Is(err, ErrNoTask) {
		t.Fatalf("before not_before: Lease = %v, want ErrNoTask", err)
	}
	f.now = future.Add(time.Second)
	task, err := f.q.Lease(f.ctx, "w1")
	if err != nil || task == nil {
		t.Fatalf("after not_before: Lease = %v %v", task, err)
	}
}
