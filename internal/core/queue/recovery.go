package queue

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"time"
)

// LeasedCount returns how many tasks workerID currently holds.
func (s *Store) LeasedCount(ctx context.Context, workerID string) (int, error) {
	var n int
	err := s.db().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM tasks WHERE status = 'leased' AND worker_id = ?`, workerID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count leased: %w", err)
	}
	return n, nil
}

// ReleaseWorker returns the tasks leased to a disconnected worker to the
// queue at once instead of waiting for their leases to expire. Kinds in keep
// stay leased: the worker may still deliver their result after reconnecting
// (send_video must not run twice). Tasks without attempts left fail; their
// ids are returned.
func (s *Store) ReleaseWorker(ctx context.Context, workerID string, keep ...Kind) (released int, failed []string, err error) {
	now := s.now().UnixMilli()
	err = s.st.Tx(ctx, func(tx *sql.Tx) error {
		held, err := leasedBy(ctx, tx, workerID)
		if err != nil {
			return err
		}
		for _, r := range held {
			if slices.Contains(keep, r.kind) {
				continue
			}
			if r.attempts < r.maxAttempts {
				_, err = tx.ExecContext(ctx,
					`UPDATE tasks SET status = 'queued', worker_id = NULL, lease_until = NULL,
					 error_code = 'worker_disconnected', not_before = ?, updated_at = ? WHERE id = ?`,
					now, now, r.id)
				released++
			} else {
				_, err = tx.ExecContext(ctx,
					`UPDATE tasks SET status = 'failed', error_code = 'worker_unavailable',
					 error_message = 'worker disconnected, max attempts reached', updated_at = ? WHERE id = ?`,
					now, r.id)
				failed = append(failed, r.id)
			}
			if err != nil {
				return fmt.Errorf("release %s: %w", r.id, err)
			}
		}
		return nil
	})
	return released, failed, err
}

// ExpireQueued fails tasks that have been ready to run since before cutoff
// without any worker taking them (docs/02-architecture.md: ten minutes
// without workers → worker_unavailable). It returns their ids.
func (s *Store) ExpireQueued(ctx context.Context, cutoff time.Time) ([]string, error) {
	var ids []string
	err := s.st.Tx(ctx, func(tx *sql.Tx) error {
		var err error
		if ids, err = readyBefore(ctx, tx, cutoff); err != nil {
			return err
		}
		now := s.now().UnixMilli()
		for _, id := range ids {
			if _, err := tx.ExecContext(ctx,
				`UPDATE tasks SET status = 'failed', error_code = 'worker_unavailable',
				 error_message = 'no worker took the task', updated_at = ? WHERE id = ? AND status = 'queued'`,
				now, id); err != nil {
				return fmt.Errorf("expire %s: %w", id, err)
			}
		}
		return nil
	})
	return ids, err
}

// ErrFinished is returned by CompleteLate for a task that already ended.
var ErrFinished = errors.New("task already finished")

// CompleteLate records a success reported by a worker that no longer holds
// the task (its result arrived after a reconnect, the task was requeued or
// handed to another worker). prevWorker is the worker leasing it now, if any,
// so the caller can cancel the duplicate run.
func (s *Store) CompleteLate(ctx context.Context, taskID string, result []byte) (prevWorker string, err error) {
	err = s.st.Tx(ctx, func(tx *sql.Tx) error {
		var status string
		var worker sql.NullString
		err := tx.QueryRowContext(ctx, `SELECT status, worker_id FROM tasks WHERE id = ?`, taskID).Scan(&status, &worker)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrFinished
		}
		if err != nil {
			return fmt.Errorf("find task: %w", err)
		}
		if Status(status) != StatusQueued && Status(status) != StatusLeased {
			return ErrFinished
		}
		if Status(status) == StatusLeased {
			prevWorker = worker.String
		}
		_, err = tx.ExecContext(ctx,
			`UPDATE tasks SET status = 'done', result = ?, error_code = NULL, error_message = NULL,
			 lease_until = NULL, updated_at = ? WHERE id = ?`,
			result, s.now().UnixMilli(), taskID)
		return err
	})
	return prevWorker, err
}

type heldTask struct {
	id                    string
	kind                  Kind
	attempts, maxAttempts int
}

func leasedBy(ctx context.Context, tx *sql.Tx, workerID string) ([]heldTask, error) {
	rows, err := tx.QueryContext(ctx,
		`SELECT id, kind, attempts, max_attempts FROM tasks WHERE status = 'leased' AND worker_id = ?`, workerID)
	if err != nil {
		return nil, fmt.Errorf("find leased: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []heldTask
	for rows.Next() {
		var r heldTask
		if err := rows.Scan(&r.id, &r.kind, &r.attempts, &r.maxAttempts); err != nil {
			return nil, fmt.Errorf("scan leased: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func readyBefore(ctx context.Context, tx *sql.Tx, cutoff time.Time) ([]string, error) {
	rows, err := tx.QueryContext(ctx,
		`SELECT id FROM tasks WHERE status = 'queued' AND not_before < ?`, cutoff.UnixMilli())
	if err != nil {
		return nil, fmt.Errorf("find stale queued: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan stale queued: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
