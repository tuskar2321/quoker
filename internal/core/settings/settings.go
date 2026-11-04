// Package settings stores runtime settings and secrets of the core in the
// settings table (docs/05-data-model.md). Values are never logged.
package settings

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/tuskar2321/quoker/internal/shared/errs"
)

// Key is a known settings key.
type Key string

// Known keys.
const (
	BotToken                Key = "bot_token"
	InstagramCookies        Key = "instagram_cookies"
	BotUsername             Key = "bot_username"
	AdminChatID             Key = "admin_chat_id"
	PollerWorkerID          Key = "poller_worker_id"
	MinSupportedVersionCode Key = "min_supported_version_code"
)

var known = []Key{BotToken, InstagramCookies, BotUsername, AdminChatID, PollerWorkerID, MinSupportedVersionCode}

// ParseKey validates a key name (e.g. from the CLI).
func ParseKey(s string) (Key, error) {
	k := Key(s)
	if !slices.Contains(known, k) {
		return "", fmt.Errorf("unknown settings key %q", s)
	}
	return k, nil
}

// Store reads and writes settings.
type Store struct {
	db  *sql.DB
	now func() time.Time
}

// New returns a Store; now defaults to time.Now.
func New(db *sql.DB, now func() time.Time) *Store {
	if now == nil {
		now = time.Now
	}
	return &Store{db: db, now: now}
}

// Get returns the value of key and whether it is set.
func (s *Store) Get(ctx context.Context, key Key) (string, bool, error) {
	var v string
	err := s.db.QueryRowContext(ctx, "SELECT value FROM settings WHERE key = ?", string(key)).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("get setting %s: %w", key, err)
	}
	return v, true, nil
}

// Set stores value under key.
func (s *Store) Set(ctx context.Context, key Key, value string) error {
	if err := validate(key, value); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)
		 ON CONFLICT (key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		string(key), value, s.now().UnixMilli())
	if err != nil {
		return fmt.Errorf("set setting %s: %w", key, err)
	}
	return nil
}

// Int returns an integer setting, or def when unset.
func (s *Store) Int(ctx context.Context, key Key, def int64) (int64, error) {
	v, ok, err := s.Get(ctx, key)
	if err != nil || !ok {
		return def, err
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return def, fmt.Errorf("setting %s is not an integer: %w", key, err)
	}
	return n, nil
}

// validate rejects values that would break readers: a non-numeric
// min_supported_version_code fails every versioned request with 500, a
// broken admin_chat_id silently stops alerts.
func validate(key Key, value string) error {
	switch key {
	case MinSupportedVersionCode:
		if n, err := strconv.ParseInt(value, 10, 64); err != nil || n < 0 {
			return errs.New(errs.InvalidRequest, fmt.Sprintf("%s must be a non-negative integer, got %q", key, value))
		}
	case AdminChatID:
		if _, err := strconv.ParseInt(value, 10, 64); err != nil {
			return errs.New(errs.InvalidRequest, fmt.Sprintf("%s must be a Telegram chat id, got %q", key, value))
		}
	}
	return nil
}
