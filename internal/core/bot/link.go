// Package bot contains the business logic of the Telegram bot: account
// linking, target management, and command dispatch.
// See docs/06-telegram-bot.md.
package bot

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/tuskar2321/quoker/internal/core/settings"
	"github.com/tuskar2321/quoker/internal/core/store"
	"github.com/tuskar2321/quoker/internal/shared/errs"
)

const (
	linkTokenTTL  = 10 * time.Minute
	linkTokenSize = 32 // bytes of random material → ~43-char base64url
)

// LinkStatus is the state of a pending link token.
type LinkStatus string

// Link status values.
const (
	LinkPending LinkStatus = "pending"
	LinkLinked  LinkStatus = "linked"
	LinkExpired LinkStatus = "expired"
)

// LinkInfo is returned by CreateLinkToken.
type LinkInfo struct {
	DeepLink  string // "https://t.me/<bot>?start=link_<token>"
	ExpiresAt time.Time
}

// LinkStatusInfo is returned by TelegramLinkStatus.
type LinkStatusInfo struct {
	Status    LinkStatus
	Username  string // non-empty when linked
	FirstName string // non-empty when linked
}

// Service handles Telegram-related business logic.
type Service struct {
	st  *store.Store
	set *settings.Store
	now func() time.Time
	rnd func([]byte) (int, error) // crypto/rand.Read in production
}

// New returns a Service.
func New(st *store.Store, set *settings.Store, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{st: st, set: set, now: now, rnd: rand.Read}
}

// CreateLinkToken generates a fresh deep-link token for the given user, stores
// its SHA-256 hash, and returns the full deep link. Existing unused tokens for
// the same user are invalidated (marked used_at = now).
func (s *Service) CreateLinkToken(ctx context.Context, userID int64) (LinkInfo, error) {
	botUsername, ok, err := s.set.Get(ctx, settings.BotUsername)
	if err != nil {
		return LinkInfo{}, fmt.Errorf("get bot_username: %w", err)
	}
	if !ok || botUsername == "" {
		return LinkInfo{}, errs.New(errs.WorkerUnavailable, "bot not configured")
	}

	raw := make([]byte, linkTokenSize)
	if _, err := s.rnd(raw); err != nil {
		return LinkInfo{}, fmt.Errorf("generate token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	hash := sha256.Sum256([]byte(token))

	now := s.now()
	expires := now.Add(linkTokenTTL)

	var info LinkInfo
	err = s.st.Tx(ctx, func(tx *sql.Tx) error {
		_, txErr := tx.ExecContext(ctx,
			`UPDATE link_tokens SET used_at = ? WHERE user_id = ? AND used_at IS NULL`,
			now.UnixMilli(), userID)
		if txErr != nil {
			return fmt.Errorf("revoke old tokens: %w", txErr)
		}
		_, txErr = tx.ExecContext(ctx,
			`INSERT INTO link_tokens (token_hash, user_id, created_at, expires_at)
			 VALUES (?, ?, ?, ?)`,
			hash[:], userID, now.UnixMilli(), expires.UnixMilli())
		if txErr != nil {
			return fmt.Errorf("insert token: %w", txErr)
		}
		info = LinkInfo{
			DeepLink:  fmt.Sprintf("https://t.me/%s?start=link_%s", botUsername, token),
			ExpiresAt: expires,
		}
		return nil
	})
	return info, err
}

// TelegramLinkStatus checks whether a pending link token has been fulfilled.
// Status is "linked" once the bot has set tg_user_id on the user.
func (s *Service) TelegramLinkStatus(ctx context.Context, userID int64) (LinkStatusInfo, error) {
	now := s.now()

	// 1. Check whether the user already has Telegram linked.
	var tgID sql.NullInt64
	var username, firstName sql.NullString
	err := s.st.DB().QueryRowContext(ctx,
		`SELECT tg_user_id, tg_username, tg_first_name FROM users WHERE id = ?`, userID).
		Scan(&tgID, &username, &firstName)
	if errors.Is(err, sql.ErrNoRows) {
		return LinkStatusInfo{}, errs.New(errs.NotFound, "user not found")
	}
	if err != nil {
		return LinkStatusInfo{}, fmt.Errorf("query user: %w", err)
	}
	if tgID.Valid && tgID.Int64 != 0 {
		return LinkStatusInfo{
			Status:    LinkLinked,
			Username:  username.String,
			FirstName: firstName.String,
		}, nil
	}

	// 2. Check whether a live (unused) token exists.
	var expiresMS int64
	err = s.st.DB().QueryRowContext(ctx,
		`SELECT expires_at FROM link_tokens
		 WHERE user_id = ? AND used_at IS NULL
		 ORDER BY created_at DESC LIMIT 1`, userID).Scan(&expiresMS)
	if errors.Is(err, sql.ErrNoRows) {
		return LinkStatusInfo{Status: LinkExpired}, nil
	}
	if err != nil {
		return LinkStatusInfo{}, fmt.Errorf("query link_tokens: %w", err)
	}
	if time.UnixMilli(expiresMS).Before(now) {
		return LinkStatusInfo{Status: LinkExpired}, nil
	}
	return LinkStatusInfo{Status: LinkPending}, nil
}

// UnlinkTelegram removes the Telegram binding from a user and deactivates all
// their targets. The reason is user_left, not bot_removed: re-adding the bot
// to a chat must not revive targets of someone who unlinked.
func (s *Service) UnlinkTelegram(ctx context.Context, userID int64) error {
	now := s.now()
	return s.st.Tx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			`UPDATE users SET tg_user_id = NULL, tg_username = NULL, tg_first_name = NULL,
			 tg_linked_at = NULL WHERE id = ?`, userID)
		if err != nil {
			return fmt.Errorf("clear tg fields: %w", err)
		}
		_, err = tx.ExecContext(ctx,
			`UPDATE targets SET status = 'inactive', inactive_reason = 'user_left',
			 rights_checked_at = NULL, updated_at = ? WHERE owner_user_id = ? AND status = 'active'`,
			now.UnixMilli(), userID)
		if err != nil {
			return fmt.Errorf("deactivate targets: %w", err)
		}
		return nil
	})
}

// ConsumeLinkToken validates a raw token (from /start link_<token>), marks it
// used, and links the given Telegram account to the token's owner. It also
// creates a "private" target (the bot DM). Returns the linked user ID on
// success.
//
// Errors: errs.LinkTokenExpired (expired/used token), errs.Forbidden (tg_user_id
// already linked to a different account).
func (s *Service) ConsumeLinkToken(ctx context.Context, rawToken string, tgUserID int64, tgUsername, tgFirstName string) (int64, error) {
	hash := sha256.Sum256([]byte(rawToken))
	now := s.now()

	var userID int64
	err := s.st.Tx(ctx, func(tx *sql.Tx) error {
		var expiresMS int64
		var usedAt sql.NullInt64
		err := tx.QueryRowContext(ctx,
			`SELECT user_id, expires_at, used_at FROM link_tokens WHERE token_hash = ?`,
			hash[:]).Scan(&userID, &expiresMS, &usedAt)
		if errors.Is(err, sql.ErrNoRows) {
			return errs.New(errs.LinkTokenExpired, "token not found")
		}
		if err != nil {
			return fmt.Errorf("query token: %w", err)
		}
		if usedAt.Valid || time.UnixMilli(expiresMS).Before(now) {
			return errs.New(errs.LinkTokenExpired, "token expired or used")
		}

		// Check whether this Telegram id is already bound to another user.
		var existingUser sql.NullInt64
		err = tx.QueryRowContext(ctx,
			`SELECT id FROM users WHERE tg_user_id = ? AND id != ?`, tgUserID, userID).
			Scan(&existingUser)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("check existing tg binding: %w", err)
		}
		if existingUser.Valid {
			return errs.New(errs.Forbidden, "telegram id already bound to another account")
		}

		// Linking a different Telegram account: the old bot chat belongs to
		// someone else now, and rights checked for the old account no longer
		// say anything about the new one.
		if _, err = tx.ExecContext(ctx,
			`UPDATE targets SET status = 'inactive', inactive_reason = 'user_left', updated_at = ?
			 WHERE owner_user_id = ? AND type = 'private' AND chat_id != ? AND status = 'active'`,
			now.UnixMilli(), userID, tgUserID); err != nil {
			return fmt.Errorf("deactivate old bot chat: %w", err)
		}
		if _, err = tx.ExecContext(ctx,
			`UPDATE targets SET rights_checked_at = NULL WHERE owner_user_id = ?`, userID); err != nil {
			return fmt.Errorf("reset rights cache: %w", err)
		}

		// Mark token used.
		if _, err = tx.ExecContext(ctx,
			`UPDATE link_tokens SET used_at = ? WHERE token_hash = ?`,
			now.UnixMilli(), hash[:]); err != nil {
			return fmt.Errorf("mark token used: %w", err)
		}

		// Bind Telegram to the user.
		if _, err = tx.ExecContext(ctx,
			`UPDATE users SET tg_user_id = ?, tg_username = ?, tg_first_name = ?,
			 tg_linked_at = ? WHERE id = ?`,
			tgUserID, nullableStr(tgUsername), nullableStr(tgFirstName),
			now.UnixMilli(), userID); err != nil {
			return fmt.Errorf("link tg: %w", err)
		}

		// Upsert the private target (bot DM) — the bot's own chat with the user.
		if _, err = tx.ExecContext(ctx,
			`INSERT INTO targets
			   (owner_user_id, chat_id, type, title, status, created_at, updated_at)
			 VALUES (?, ?, 'private', 'Сохранённое в боте', 'active', ?, ?)
			 ON CONFLICT(owner_user_id, chat_id) DO UPDATE SET
			   status = 'active', inactive_reason = NULL, updated_at = excluded.updated_at`,
			userID, tgUserID, now.UnixMilli(), now.UnixMilli()); err != nil {
			return fmt.Errorf("upsert private target: %w", err)
		}
		return nil
	})
	return userID, err
}

func nullableStr(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}
