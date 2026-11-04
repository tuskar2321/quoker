// Package targets manages Telegram chat targets for video delivery.
// See docs/06-telegram-bot.md and docs/03-public-api.md.
package targets

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/tuskar2321/quoker/internal/core/store"
	"github.com/tuskar2321/quoker/internal/shared/errs"
)

// Status mirrors the DB check constraint.
type Status string

// Target statuses.
const (
	StatusActive   Status = "active"
	StatusInactive Status = "inactive"
)

// InactiveReason mirrors the DB check constraint.
type InactiveReason string

// Inactive reasons.
const (
	ReasonBotRemoved   InactiveReason = "bot_removed"
	ReasonBotNotAdmin  InactiveReason = "bot_not_admin"
	ReasonUserLeft     InactiveReason = "user_left"
	ReasonUserNotAdmin InactiveReason = "user_not_admin"
)

// Target is a row from the targets table.
type Target struct {
	ID              int64
	OwnerUserID     int64
	ChatID          int64
	Type            string // group | channel | private
	Title           string
	Status          Status
	InactiveReason  InactiveReason
	Hidden          bool
	RightsCheckedAt time.Time // zero when never checked
	LastUsedAt      time.Time // zero when never used
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// Service provides target operations.
type Service struct {
	st         *store.Store
	now        func() time.Time
	bot        BotCaller // nil skips rights checks
	rightsWait time.Duration
}

// BotCaller runs a Bot API method on a worker and returns its result.
type BotCaller interface {
	Call(ctx context.Context, method string, params any) (json.RawMessage, error)
}

// WithBot enables rights checks through b.
func (s *Service) WithBot(b BotCaller) *Service {
	s.bot = b
	return s
}

// New returns a Service.
func New(st *store.Store, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{st: st, now: now, rightsWait: DefaultRightsWait}
}

// WithRightsWait overrides the getChatMember budget (for tests).
func (s *Service) WithRightsWait(d time.Duration) *Service {
	s.rightsWait = d
	return s
}

// List returns all targets for userID. By default hidden targets are excluded;
// pass includeHidden=true to include them. Sorted: last used first, then by
// title.
func (s *Service) List(ctx context.Context, userID int64, includeHidden bool) ([]Target, error) {
	q := `SELECT id, owner_user_id, chat_id, type, title, status, inactive_reason,
		         hidden, rights_checked_at, last_used_at, created_at, updated_at
		  FROM targets
		  WHERE owner_user_id = ?`
	if !includeHidden {
		q += ` AND hidden = 0`
	}
	q += ` ORDER BY last_used_at DESC NULLS LAST, title ASC`

	rows, err := s.st.DB().QueryContext(ctx, q, userID)
	if err != nil {
		return nil, fmt.Errorf("list targets: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Target
	for rows.Next() {
		t, err := scanTarget(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// Get returns a single target by id, verifying ownership.
func (s *Service) Get(ctx context.Context, userID, targetID int64) (Target, error) {
	row := s.st.DB().QueryRowContext(ctx, `
		SELECT id, owner_user_id, chat_id, type, title, status, inactive_reason,
		       hidden, rights_checked_at, last_used_at, created_at, updated_at
		FROM targets WHERE id = ? AND owner_user_id = ?`, targetID, userID)
	t, err := scanTarget(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Target{}, errs.New(errs.NotFound, "target not found")
	}
	return t, err
}

// SetHidden updates the hidden flag of a target owned by userID.
func (s *Service) SetHidden(ctx context.Context, userID, targetID int64, hidden bool) (Target, error) {
	now := s.now()
	res, err := s.st.DB().ExecContext(ctx,
		`UPDATE targets SET hidden = ?, updated_at = ? WHERE id = ? AND owner_user_id = ?`,
		boolInt(hidden), now.UnixMilli(), targetID, userID)
	if err != nil {
		return Target{}, fmt.Errorf("update hidden: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return Target{}, errs.New(errs.NotFound, "target not found")
	}
	return s.Get(ctx, userID, targetID)
}

// scanner is a common interface for *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

func scanTarget(s scanner) (Target, error) {
	var (
		t              Target
		status         string
		inactiveReason sql.NullString
		hidden         int64
		checkedMS      sql.NullInt64
		lastUsedMS     sql.NullInt64
		createdMS      int64
		updatedMS      int64
	)
	err := s.Scan(
		&t.ID, &t.OwnerUserID, &t.ChatID, &t.Type, &t.Title,
		&status, &inactiveReason, &hidden,
		&checkedMS, &lastUsedMS, &createdMS, &updatedMS)
	if err != nil {
		return Target{}, fmt.Errorf("scan target: %w", err)
	}
	t.Status = Status(status)
	t.InactiveReason = InactiveReason(inactiveReason.String)
	t.Hidden = hidden != 0
	if lastUsedMS.Valid {
		t.LastUsedAt = time.UnixMilli(lastUsedMS.Int64).UTC()
	}
	if checkedMS.Valid {
		t.RightsCheckedAt = time.UnixMilli(checkedMS.Int64).UTC()
	}
	t.CreatedAt = time.UnixMilli(createdMS).UTC()
	t.UpdatedAt = time.UnixMilli(updatedMS).UTC()
	return t, nil
}

// Upsert inserts or updates a target. Used by the bot to register new chats.
// chatID uniquely identifies the chat; if a target for (ownerUserID, chatID)
// already exists it is updated with the new metadata.
func (s *Service) Upsert(ctx context.Context, t Target) (Target, error) {
	now := s.now()
	res, err := s.st.DB().ExecContext(ctx, `
		INSERT INTO targets
		  (owner_user_id, chat_id, type, title, status, hidden, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 0, ?, ?)
		ON CONFLICT(owner_user_id, chat_id) DO UPDATE SET
		  type            = excluded.type,
		  title           = excluded.title,
		  status          = excluded.status,
		  inactive_reason = CASE WHEN excluded.status = 'active' THEN NULL ELSE inactive_reason END,
		  updated_at      = excluded.updated_at`,
		t.OwnerUserID, t.ChatID, t.Type, t.Title, string(t.Status),
		now.UnixMilli(), now.UnixMilli())
	if err != nil {
		return Target{}, fmt.Errorf("upsert target: %w", err)
	}
	id, _ := res.LastInsertId()
	if id == 0 {
		// ON CONFLICT path: fetch the existing id.
		err = s.st.DB().QueryRowContext(ctx,
			`SELECT id FROM targets WHERE owner_user_id = ? AND chat_id = ?`,
			t.OwnerUserID, t.ChatID).Scan(&id)
		if err != nil {
			return Target{}, fmt.Errorf("fetch upserted id: %w", err)
		}
	}
	return s.Get(ctx, t.OwnerUserID, id)
}

// SetStatus updates the status and optional inactive_reason of targets
// matching chatID. Used by the bot on my_chat_member events.
func (s *Service) SetStatus(ctx context.Context, chatID int64, status Status, reason InactiveReason) error {
	now := s.now()
	_, err := s.st.DB().ExecContext(ctx,
		`UPDATE targets SET status = ?, inactive_reason = ?, updated_at = ?
		 WHERE chat_id = ?`,
		string(status), nullableReason(reason), now.UnixMilli(), chatID)
	return err
}

// UpdateChatID replaces chat_id across all targets (group → supergroup migration).
// Owners who already have a target for the new chat (they ran /connect
// there before the migration message arrived) keep that one; their old
// target cannot be deleted because sends refer to it, so it is switched off.
func (s *Service) UpdateChatID(ctx context.Context, oldChatID, newChatID int64) error {
	now := s.now().UnixMilli()
	return s.st.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`UPDATE targets SET status = 'inactive', inactive_reason = 'bot_removed', updated_at = ?
			 WHERE chat_id = ? AND owner_user_id IN (SELECT owner_user_id FROM targets WHERE chat_id = ?)`,
			now, oldChatID, newChatID); err != nil {
			return fmt.Errorf("retire duplicate targets: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE targets SET chat_id = ?, updated_at = ?
			 WHERE chat_id = ? AND owner_user_id NOT IN (SELECT owner_user_id FROM targets WHERE chat_id = ?)`,
			newChatID, now, oldChatID, newChatID); err != nil {
			return fmt.Errorf("move targets to the new chat: %w", err)
		}
		return nil
	})
}

// UpdateTitle updates the title of targets with the given chat_id.
func (s *Service) UpdateTitle(ctx context.Context, chatID int64, title string) error {
	now := s.now()
	_, err := s.st.DB().ExecContext(ctx,
		`UPDATE targets SET title = ?, updated_at = ? WHERE chat_id = ?`,
		title, now.UnixMilli(), chatID)
	return err
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func nullableReason(r InactiveReason) sql.NullString {
	if r == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: string(r), Valid: true}
}
