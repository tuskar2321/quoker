package queue

import (
	"errors"
	"testing"
	"time"
)

func TestReleaseWorker(t *testing.T) {
	f := newFixture(t)
	resolve := f.enqueue(KindResolve, "")
	send := f.enqueue(KindSendVideo, "")
	for range 2 {
		if _, err := f.q.Lease(f.ctx, "w1"); err != nil {
			t.Fatal(err)
		}
	}
	if n, _ := f.q.LeasedCount(f.ctx, "w1"); n != 2 {
		t.Fatalf("leased = %d", n)
	}
	released, failed, err := f.q.ReleaseWorker(f.ctx, "w1", KindSendVideo)
	if err != nil || released != 1 || len(failed) != 0 {
		t.Fatalf("release: %d %v %v", released, failed, err)
	}
	if got, _ := f.q.Get(f.ctx, resolve); got.Status != StatusQueued || !got.NotBefore.Equal(f.now) {
		t.Fatalf("resolve after release = %s, not_before %s", got.Status, got.NotBefore)
	}
	if got, _ := f.q.Get(f.ctx, send); got.Status != StatusLeased {
		t.Fatalf("send_video must stay leased, got %s", got.Status)
	}
}

func TestReleaseWorker_outOfAttempts(t *testing.T) {
	f := newFixture(t)
	id := f.enqueue(KindResolve, "")
	for range 3 { // max_attempts for resolve
		if _, err := f.q.Lease(f.ctx, "w1"); err != nil {
			t.Fatal(err)
		}
		if _, failed, _ := f.q.ReleaseWorker(f.ctx, "w1"); len(failed) > 0 {
			if failed[0] != id {
				t.Fatalf("failed = %v", failed)
			}
			got, _ := f.q.Get(f.ctx, id)
			if got.Status != StatusFailed || got.ErrorCode != "worker_unavailable" {
				t.Fatalf("task = %+v", got)
			}
			return
		}
	}
	t.Fatal("task never failed")
}

func TestExpireQueued(t *testing.T) {
	f := newFixture(t)
	old := f.enqueue(KindResolve, "")
	f.now = f.now.Add(11 * time.Minute)
	fresh := f.enqueue(KindSendVideo, "")
	ids, err := f.q.ExpireQueued(f.ctx, f.now.Add(-10*time.Minute))
	if err != nil || len(ids) != 1 || ids[0] != old {
		t.Fatalf("expired = %v, %v", ids, err)
	}
	if got, _ := f.q.Get(f.ctx, fresh); got.Status != StatusQueued {
		t.Fatalf("fresh task = %s", got.Status)
	}
}

func TestCompleteLate(t *testing.T) {
	f := newFixture(t)
	id := f.enqueue(KindSendVideo, "")
	if _, err := f.q.Lease(f.ctx, "w2"); err != nil { // the rerun after w1 vanished
		t.Fatal(err)
	}
	prev, err := f.q.CompleteLate(f.ctx, id, []byte("r"))
	if err != nil || prev != "w2" {
		t.Fatalf("prev = %q, err = %v", prev, err)
	}
	got, _ := f.q.Get(f.ctx, id)
	if got.Status != StatusDone || string(got.Result) != "r" {
		t.Fatalf("task = %+v", got)
	}
	if _, err := f.q.CompleteLate(f.ctx, id, nil); !errors.Is(err, ErrFinished) {
		t.Fatalf("second late result: %v", err)
	}
}

func TestRenewLeaseKeepsProgress(t *testing.T) {
	f := newFixture(t)
	id := f.enqueue(KindDownload, "")
	_, _ = f.q.Lease(f.ctx, "w1")
	_ = f.q.SetProgress(f.ctx, id, "w1", 40)
	_ = f.q.RenewLease(f.ctx, id, "w1")
	if got, _ := f.q.Get(f.ctx, id); got.Progress != 40 {
		t.Fatalf("progress = %d, want 40", got.Progress)
	}
}
