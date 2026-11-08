package queue

import (
	"errors"
	"testing"
	"time"
)

// enqueueDownload queues a download on behalf of userID (0: not user-initiated).
func (f *fixture) enqueueDownload(userID int64) string {
	f.t.Helper()
	id, err := f.q.Enqueue(f.ctx, EnqueueParams{Kind: KindDownload, Payload: []byte("p"), UserID: userID})
	if err != nil {
		f.t.Fatalf("Enqueue: %v", err)
	}
	f.now = f.now.Add(time.Millisecond) // distinct created_at keeps FIFO order explicit
	return id
}

func (f *fixture) addUsers(ids ...int64) {
	f.t.Helper()
	for _, id := range ids {
		if _, err := f.q.st.DB().ExecContext(f.ctx,
			`INSERT INTO users (id, role, status, created_at) VALUES (?, 'user', 'active', 0)`, id); err != nil {
			f.t.Fatal(err)
		}
	}
}

func (f *fixture) leaseID() string {
	f.t.Helper()
	task, err := f.q.Lease(f.ctx, "w1")
	if errors.Is(err, ErrNoTask) {
		return ""
	}
	if err != nil {
		f.t.Fatal(err)
	}
	return task.ID
}

func (f *fixture) hasReady() bool {
	f.t.Helper()
	ok, err := f.q.HasReady(f.ctx)
	if err != nil {
		f.t.Fatal(err)
	}
	return ok
}

func TestLease_capsActiveDownloadsPerUser(t *testing.T) {
	f := newFixture(t)
	f.addUsers(1, 2)
	a1 := f.enqueueDownload(1)
	a2 := f.enqueueDownload(1)
	a3 := f.enqueueDownload(1)
	b1 := f.enqueueDownload(2)

	// User 1 gets MaxActiveDownloadsPerUser slots; their third download waits
	// and user 2's later download goes ahead of it.
	for i, want := range []string{a1, a2, b1} {
		if got := f.leaseID(); got != want {
			t.Fatalf("lease %d = %q, want %q", i, got, want)
		}
	}
	if got := f.leaseID(); got != "" {
		t.Fatalf("lease while user 1 is at the cap = %q, want none", got)
	}
	if f.hasReady() {
		t.Fatal("HasReady = true while the only queued task is held back by the cap")
	}

	// A finished download frees a slot for the waiting one.
	if err := f.q.Complete(f.ctx, CompleteParams{TaskID: a1, WorkerID: "w1"}); err != nil {
		t.Fatal(err)
	}
	if !f.hasReady() {
		t.Fatal("HasReady = false after a slot was freed")
	}
	if got := f.leaseID(); got != a3 {
		t.Fatalf("lease after a slot was freed = %q, want %q", got, a3)
	}
}

func TestLease_capIgnoresOtherKindsAndSystemTasks(t *testing.T) {
	f := newFixture(t)
	f.addUsers(1)
	f.enqueueDownload(1)
	f.enqueueDownload(1)
	held := f.enqueueDownload(1)
	system := f.enqueueDownload(0) // not user-initiated: never capped
	send, err := f.q.Enqueue(f.ctx, EnqueueParams{Kind: KindSendVideo, Payload: []byte("p"), UserID: 1})
	if err != nil {
		t.Fatal(err)
	}

	got := map[string]bool{}
	for {
		id := f.leaseID()
		if id == "" {
			break
		}
		got[id] = true
	}
	if !got[send] {
		t.Fatal("send_video of a user at the download cap was not leased")
	}
	if !got[system] {
		t.Fatal("download without a user was not leased")
	}
	if got[held] {
		t.Fatal("third download of user 1 was leased past the cap")
	}
	if len(got) != 4 {
		t.Fatalf("leased %d tasks, want 4 (two downloads of user 1, system download, send)", len(got))
	}
}
