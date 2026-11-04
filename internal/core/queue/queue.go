// Package queue manages the SQLite task queue for core workers.
// See docs/05-data-model.md (tasks table) and docs/04-core-worker-protocol.md.
package queue

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/tuskar2321/quoker/internal/core/store"
	"github.com/tuskar2321/quoker/internal/shared/errs"
)

// Kind is a task type.
type Kind string

// Task kinds as defined in docs/05-data-model.md.
const (
	KindResolve   Kind = "resolve"
	KindDownload  Kind = "download"
	KindSendVideo Kind = "send_video"
	KindBotCall   Kind = "bot_call"
)

// Priority values match docs/05-data-model.md.
const (
	PriorityBotCall   = 30
	PriorityResolve   = 20
	PrioritySendVideo = 10
	PriorityDownload  = 0
)

// defaultPriority maps kinds to their priorities.
func defaultPriority(k Kind) int {
	switch k {
	case KindBotCall:
		return PriorityBotCall
	case KindResolve:
		return PriorityResolve
	case KindSendVideo:
		return PrioritySendVideo
	}
	return PriorityDownload
}

// defaultMaxAttempts per kind (docs/04-core-worker-protocol.md).
func defaultMaxAttempts(k Kind) int {
	switch k {
	case KindResolve, KindBotCall, KindDownload:
		return 3 // initial attempt + 2 retries
	case KindSendVideo:
		return 2 // initial + 1 retry
	}
	return 1
}

// defaultLease per kind.
func defaultLease(k Kind) time.Duration {
	switch k {
	case KindResolve:
		return 60 * time.Second
	case KindDownload, KindSendVideo:
		return 10 * time.Minute
	case KindBotCall:
		return 30 * time.Second
	}
	return 60 * time.Second
}

// Status of a task.
type Status string

// Task statuses.
const (
	StatusQueued    Status = "queued"
	StatusLeased    Status = "leased"
	StatusDone      Status = "done"
	StatusFailed    Status = "failed"
	StatusCancelled Status = "cancelled"
)

// Task is a row from the tasks table.
type Task struct {
	ID          string
	Kind        Kind
	Payload     []byte
	Status      Status
	Priority    int
	Attempts    int
	MaxAttempts int
	WorkerID    string
	LeaseUntil  time.Time
	Progress    int
	Result      []byte
	ErrorCode   string
	ErrorMsg    string
	DedupKey    string
	UserID      int64
	NotBefore   time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// EnqueueParams are the parameters for Enqueue.
type EnqueueParams struct {
	Kind        Kind
	Payload     []byte    // protobuf-encoded task
	DedupKey    string    // empty for tasks without dedup
	UserID      int64     // 0 if not user-initiated
	Priority    int       // 0 means use kind default
	MaxAttempts int       // 0 means use kind default
	NotBefore   time.Time // zero means immediately
}

// Store wraps *store.Store and provides queue operations.
type Store struct {
	st  *store.Store
	now func() time.Time
}

// New returns a queue Store.
func New(st *store.Store, now func() time.Time) *Store {
	if now == nil {
		now = time.Now
	}
	return &Store{st: st, now: now}
}

func (s *Store) db() *sql.DB { return s.st.DB() }

// Enqueue inserts a task unless a queued/leased task with the same dedup_key
// already exists, in which case it returns that task's ID and ErrDuplicate.
func (s *Store) Enqueue(ctx context.Context, p EnqueueParams) (string, error) {
	var id string
	err := s.st.Tx(ctx, func(tx *sql.Tx) error {
		var err error
		id, err = s.EnqueueTx(ctx, tx, p)
		return err
	})
	if err != nil && !errors.Is(err, ErrDuplicate) {
		return "", err
	}
	return id, err
}

// EnqueueTx is Enqueue inside the caller's transaction, so a task can be
// created atomically with the domain row that references it.
func (s *Store) EnqueueTx(ctx context.Context, tx *sql.Tx, p EnqueueParams) (string, error) {
	if p.Priority == 0 {
		p.Priority = defaultPriority(p.Kind)
	}
	if p.MaxAttempts == 0 {
		p.MaxAttempts = defaultMaxAttempts(p.Kind)
	}
	now := s.now()
	notBefore := p.NotBefore
	if notBefore.IsZero() {
		notBefore = now
	}
	if p.DedupKey != "" {
		var existing string
		err := tx.QueryRowContext(ctx,
			`SELECT id FROM tasks WHERE dedup_key = ? AND status IN ('queued','leased')`,
			p.DedupKey).Scan(&existing)
		if err == nil {
			return existing, ErrDuplicate
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return "", fmt.Errorf("check dedup: %w", err)
		}
	}
	id := newULID(now)
	_, err := tx.ExecContext(ctx,
		`INSERT INTO tasks
		 (id, kind, payload, status, priority, attempts, max_attempts, dedup_key,
		  user_id, not_before, created_at, updated_at)
		 VALUES (?, ?, ?, 'queued', ?, 0, ?, ?, ?, ?, ?, ?)`,
		id, string(p.Kind), p.Payload, p.Priority, p.MaxAttempts,
		nullStr(p.DedupKey), nullInt64(p.UserID),
		notBefore.UnixMilli(), now.UnixMilli(), now.UnixMilli())
	if err != nil {
		return "", fmt.Errorf("insert task: %w", err)
	}
	return id, nil
}

// ErrDuplicate is returned by Enqueue when a dedup key already has an active task.
var ErrDuplicate = errors.New("duplicate task")

// Lease picks the highest-priority ready task, marks it leased to workerID, and
// returns it. Returns ErrNoTask if there is nothing to pick.
func (s *Store) Lease(ctx context.Context, workerID string) (*Task, error) {
	now := s.now()
	var t Task
	err := s.st.Tx(ctx, func(tx *sql.Tx) error {
		var (
			row                                                 Task
			workerIDNull                                        sql.NullString
			leaseUntilNull, progressNull, userIDNull            sql.NullInt64
			resultNull, errorCodeNull, errorMsgNull             sql.NullString
			dedupKeyNull                                        sql.NullString
			kind, status                                        string
			notBeforeMS, createdAtMS, updatedAtMS, leaseUntilMS int64
		)
		err := tx.QueryRowContext(ctx,
			`SELECT id, kind, payload, status, priority, attempts, max_attempts,
			        worker_id, lease_until, progress, result, error_code, error_message,
			        dedup_key, user_id, not_before, created_at, updated_at
			 FROM tasks
			 WHERE status = 'queued' AND not_before <= ?
			 ORDER BY priority DESC, not_before, created_at
			 LIMIT 1`, now.UnixMilli()).Scan(
			&row.ID, &kind, &row.Payload, &status, &row.Priority,
			&row.Attempts, &row.MaxAttempts, &workerIDNull, &leaseUntilNull,
			&progressNull, &resultNull, &errorCodeNull, &errorMsgNull,
			&dedupKeyNull, &userIDNull, &notBeforeMS, &createdAtMS, &updatedAtMS)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNoTask
		}
		if err != nil {
			return fmt.Errorf("pick task: %w", err)
		}
		_ = leaseUntilMS
		row.Kind = Kind(kind)
		row.Status = Status(status)
		row.WorkerID = workerIDNull.String
		row.Progress = int(progressNull.Int64)
		row.Result = []byte(resultNull.String)
		row.ErrorCode = errorCodeNull.String
		row.ErrorMsg = errorMsgNull.String
		row.DedupKey = dedupKeyNull.String
		row.UserID = userIDNull.Int64
		row.NotBefore = time.UnixMilli(notBeforeMS).UTC()
		row.CreatedAt = time.UnixMilli(createdAtMS).UTC()
		row.UpdatedAt = time.UnixMilli(updatedAtMS).UTC()

		lease := now.Add(defaultLease(row.Kind))
		if _, err := tx.ExecContext(ctx,
			`UPDATE tasks SET status = 'leased', worker_id = ?, lease_until = ?,
			 attempts = attempts + 1, updated_at = ? WHERE id = ?`,
			workerID, lease.UnixMilli(), now.UnixMilli(), row.ID); err != nil {
			return fmt.Errorf("lease task: %w", err)
		}
		row.Status = StatusLeased
		row.WorkerID = workerID
		row.LeaseUntil = lease
		row.Attempts++
		t = row
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// ErrNoTask is returned by Lease when the queue has no ready task.
var ErrNoTask = errors.New("no task available")

// RenewLease extends the lease of a leased task. It is a no-op if the task
// is no longer leased to workerID.
func (s *Store) RenewLease(ctx context.Context, taskID, workerID string) error {
	return s.renewLease(ctx, taskID, workerID, -1)
}

// SetProgress stores task progress percent and renews the lease.
func (s *Store) SetProgress(ctx context.Context, taskID, workerID string, percent int) error {
	return s.renewLease(ctx, taskID, workerID, percent)
}

func (s *Store) renewLease(ctx context.Context, taskID, workerID string, progress int) error {
	now := s.now()
	var kindStr string
	if err := s.db().QueryRowContext(ctx,
		`SELECT kind FROM tasks WHERE id = ? AND worker_id = ? AND status = 'leased'`,
		taskID, workerID).Scan(&kindStr); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil // task gone or reassigned; ignore
		}
		return fmt.Errorf("find task kind: %w", err)
	}
	newLease := now.Add(defaultLease(Kind(kindStr)))
	// progress < 0 renews the lease and keeps the stored progress.
	_, err := s.db().ExecContext(ctx,
		`UPDATE tasks SET lease_until = ?, progress = CASE WHEN ? < 0 THEN progress ELSE ? END, updated_at = ?
		 WHERE id = ? AND worker_id = ? AND status = 'leased'`,
		newLease.UnixMilli(), progress, progress, now.UnixMilli(), taskID, workerID)
	if err != nil {
		return fmt.Errorf("renew lease: %w", err)
	}
	return nil
}

// CompleteParams is the input for Complete.
type CompleteParams struct {
	TaskID    string
	WorkerID  string
	Result    []byte // protobuf-encoded result, nil on error
	ErrCode   string // empty on success
	ErrMsg    string
	Retryable bool
}

// Complete marks a task done or failed (with retry if applicable).
func (s *Store) Complete(ctx context.Context, p CompleteParams) error {
	now := s.now()
	return s.st.Tx(ctx, func(tx *sql.Tx) error {
		var (
			attempts, maxAttempts int
			kind                  string
		)
		err := tx.QueryRowContext(ctx,
			`SELECT kind, attempts, max_attempts FROM tasks WHERE id = ? AND worker_id = ? AND status = 'leased'`,
			p.TaskID, p.WorkerID).Scan(&kind, &attempts, &maxAttempts)
		if errors.Is(err, sql.ErrNoRows) {
			return errs.New(errs.NotFound, "task not found or not leased")
		}
		if err != nil {
			return fmt.Errorf("find task: %w", err)
		}

		if p.ErrCode == "" {
			// Success.
			_, err = tx.ExecContext(ctx,
				`UPDATE tasks SET status = 'done', result = ?, error_code = NULL,
				 error_message = NULL, updated_at = ? WHERE id = ?`,
				p.Result, now.UnixMilli(), p.TaskID)
		} else if p.Retryable && attempts < maxAttempts {
			// Retry with exponential backoff: 2^(attempts-1) * 5s.
			backoff := (1 << (attempts - 1)) * 5 * int(time.Second)
			notBefore := now.Add(time.Duration(backoff))
			_, err = tx.ExecContext(ctx,
				`UPDATE tasks SET status = 'queued', worker_id = NULL, lease_until = NULL,
				 error_code = ?, error_message = ?, not_before = ?, updated_at = ?
				 WHERE id = ?`,
				p.ErrCode, p.ErrMsg, notBefore.UnixMilli(), now.UnixMilli(), p.TaskID)
		} else {
			_, err = tx.ExecContext(ctx,
				`UPDATE tasks SET status = 'failed', error_code = ?, error_message = ?,
				 updated_at = ? WHERE id = ?`,
				p.ErrCode, p.ErrMsg, now.UnixMilli(), p.TaskID)
		}
		if err != nil {
			return fmt.Errorf("complete task: %w", err)
		}
		return nil
	})
}

// Cancel marks a task cancelled if it is queued or leased by the given worker.
// Pass workerID="" to cancel regardless of who holds the lease.
func (s *Store) Cancel(ctx context.Context, taskID, workerID string) error {
	now := s.now()
	var err error
	if workerID == "" {
		_, err = s.db().ExecContext(ctx,
			`UPDATE tasks SET status = 'cancelled', updated_at = ?
			 WHERE id = ? AND status IN ('queued','leased')`,
			now.UnixMilli(), taskID)
	} else {
		_, err = s.db().ExecContext(ctx,
			`UPDATE tasks SET status = 'cancelled', updated_at = ?
			 WHERE id = ? AND worker_id = ? AND status = 'leased'`,
			now.UnixMilli(), taskID, workerID)
	}
	if err != nil {
		return fmt.Errorf("cancel task: %w", err)
	}
	return nil
}

// Get returns a task by ID.
func (s *Store) Get(ctx context.Context, id string) (*Task, error) {
	var (
		t                                         Task
		kind, status                              string
		workerIDNull                              sql.NullString
		leaseUntilNull, progressNull, userIDNull  sql.NullInt64
		resultNull                                []byte
		errorCodeNull, errorMsgNull, dedupKeyNull sql.NullString
		notBeforeMS, createdAtMS, updatedAtMS     int64
		leaseUntilMS                              sql.NullInt64
	)
	err := s.db().QueryRowContext(ctx,
		`SELECT id, kind, payload, status, priority, attempts, max_attempts,
		        worker_id, lease_until, progress, result, error_code, error_message,
		        dedup_key, user_id, not_before, created_at, updated_at
		 FROM tasks WHERE id = ?`, id).Scan(
		&t.ID, &kind, &t.Payload, &status, &t.Priority,
		&t.Attempts, &t.MaxAttempts, &workerIDNull, &leaseUntilNull,
		&progressNull, &resultNull, &errorCodeNull, &errorMsgNull,
		&dedupKeyNull, &userIDNull, &notBeforeMS, &createdAtMS, &updatedAtMS)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errs.New(errs.NotFound, "task not found")
	}
	if err != nil {
		return nil, fmt.Errorf("get task: %w", err)
	}
	_ = leaseUntilMS
	t.Progress = int(progressNull.Int64)
	t.Kind = Kind(kind)
	t.Status = Status(status)
	t.WorkerID = workerIDNull.String
	if leaseUntilNull.Valid {
		t.LeaseUntil = time.UnixMilli(leaseUntilNull.Int64).UTC()
	}
	t.Result = resultNull
	t.ErrorCode = errorCodeNull.String
	t.ErrorMsg = errorMsgNull.String
	t.DedupKey = dedupKeyNull.String
	t.UserID = userIDNull.Int64
	t.NotBefore = time.UnixMilli(notBeforeMS).UTC()
	t.CreatedAt = time.UnixMilli(createdAtMS).UTC()
	t.UpdatedAt = time.UnixMilli(updatedAtMS).UTC()
	return &t, nil
}

// RecoverExpiredLeases re-queues leased tasks whose lease has expired and
// whose attempt count is below max_attempts; otherwise marks them failed and
// returns their IDs. Should be called on a schedule (e.g. every 30 s).
func (s *Store) RecoverExpiredLeases(ctx context.Context) (recovered int, failed []string, err error) {
	now := s.now()
	return recovered, failed, s.st.Tx(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx,
			`SELECT id, kind, attempts, max_attempts FROM tasks
			 WHERE status = 'leased' AND lease_until < ?`, now.UnixMilli())
		if err != nil {
			return fmt.Errorf("find expired: %w", err)
		}
		defer func() { _ = rows.Close() }()
		type expiredRow struct {
			id          string
			kind        Kind
			attempts    int
			maxAttempts int
		}
		var expired []expiredRow
		for rows.Next() {
			var r expiredRow
			var kindStr string
			if err := rows.Scan(&r.id, &kindStr, &r.attempts, &r.maxAttempts); err != nil {
				return fmt.Errorf("scan expired: %w", err)
			}
			r.kind = Kind(kindStr)
			expired = append(expired, r)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("expired rows: %w", err)
		}
		for _, r := range expired {
			if r.attempts < r.maxAttempts {
				backoff := (1 << (r.attempts - 1)) * 5 * int(time.Second)
				notBefore := now.Add(time.Duration(backoff))
				if _, err := tx.ExecContext(ctx,
					`UPDATE tasks SET status = 'queued', worker_id = NULL, lease_until = NULL,
					 error_code = 'lease_expired', not_before = ?, updated_at = ? WHERE id = ?`,
					notBefore.UnixMilli(), now.UnixMilli(), r.id); err != nil {
					return fmt.Errorf("re-queue %s: %w", r.id, err)
				}
				recovered++
			} else {
				if _, err := tx.ExecContext(ctx,
					`UPDATE tasks SET status = 'failed', error_code = 'worker_unavailable',
					 error_message = 'lease expired, max attempts reached', updated_at = ?
					 WHERE id = ?`, now.UnixMilli(), r.id); err != nil {
					return fmt.Errorf("fail %s: %w", r.id, err)
				}
				failed = append(failed, r.id)
			}
		}
		return nil
	})
}

// newULID generates a time-sortable unique ID in ULID format (Crockford base32).
// Time part is 48-bit unix ms; random part is 80-bit crypto/rand.
func newULID(t time.Time) string {
	const enc = "0123456789ABCDEFGHJKMNPQRSTVWXYZ" // Crockford base32
	ms := uint64(t.UnixMilli())
	var rnd [10]byte
	_, _ = rand.Read(rnd[:])

	var out [26]byte
	// 10-character time part (48 bits → 50 bits in base32, top 2 unused)
	out[0] = enc[ms>>45&0x1f]
	out[1] = enc[ms>>40&0x1f]
	out[2] = enc[ms>>35&0x1f]
	out[3] = enc[ms>>30&0x1f]
	out[4] = enc[ms>>25&0x1f]
	out[5] = enc[ms>>20&0x1f]
	out[6] = enc[ms>>15&0x1f]
	out[7] = enc[ms>>10&0x1f]
	out[8] = enc[ms>>5&0x1f]
	out[9] = enc[ms&0x1f]
	// 16-character random part (80 bits)
	v := binary.BigEndian.Uint64(rnd[:8])
	for i := 15; i >= 10; i-- {
		out[i] = enc[v&0x1f]
		v >>= 5
	}
	v2 := uint64(rnd[8])<<8 | uint64(rnd[9])
	for i := 25; i >= 16; i-- {
		out[i] = enc[v2&0x1f]
		v2 >>= 5
	}
	return string(out[:])
}

func nullStr(s string) sql.NullString { return sql.NullString{String: s, Valid: s != ""} }
func nullInt64(v int64) sql.NullInt64 { return sql.NullInt64{Int64: v, Valid: v != 0} }
