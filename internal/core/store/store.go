// Package store owns the SQLite database of the core: opening, migrations
// and transactions. See docs/05-data-model.md.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"

	_ "modernc.org/sqlite" // registers the "sqlite" driver (pure Go, no CGO)

	"github.com/tuskar2321/quoker/core/migrations"
)

// Store is a handle to the core database.
type Store struct {
	db *sql.DB
}

// Open opens (creating if needed) the SQLite database at path in WAL mode
// and applies pending migrations.
func Open(ctx context.Context, path string) (*Store, error) {
	db, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		return nil, fmt.Errorf("open sqlite %q: %w", path, err)
	}
	s := &Store{db: db}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite %q: %w", path, err)
	}
	if err := migrate(ctx, db, migrations.FS()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// dsn builds a modernc.org/sqlite DSN. Pragmas are applied to every pooled
// connection; _txlock=immediate takes the write lock at BEGIN so concurrent
// writers wait on busy_timeout instead of failing on lock upgrade.
func dsn(path string) string {
	q := url.Values{}
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "synchronous(NORMAL)")
	q.Set("_txlock", "immediate")
	return "file:" + path + "?" + q.Encode()
}

// DB exposes the underlying pool for packages that own their queries.
func (s *Store) DB() *sql.DB { return s.db }

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// Ready reports whether the database answers queries; used by /readyz.
func (s *Store) Ready(ctx context.Context) error {
	var v int
	if err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&v); err != nil {
		return fmt.Errorf("query user_version: %w", err)
	}
	if v == 0 {
		return errors.New("schema is not migrated")
	}
	return nil
}

// Tx runs fn in a transaction, committing if fn returns nil.
func (s *Store) Tx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}
