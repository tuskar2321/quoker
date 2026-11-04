// Package accounts manages users, invites and device sessions.
// See docs/01-product.md (F-18, F-26, F-27) and docs/05-data-model.md.
package accounts

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/tuskar2321/quoker/internal/core/store"
	"github.com/tuskar2321/quoker/internal/shared/errs"
)

// Role of a user.
type Role string

// Roles.
const (
	RoleUser  Role = "user"
	RoleAdmin Role = "admin"
)

// ParseRole validates a role name.
func ParseRole(s string) (Role, error) {
	switch r := Role(s); r {
	case RoleUser, RoleAdmin:
		return r, nil
	}
	return "", errs.New(errs.InvalidRequest, fmt.Sprintf("unknown role %q", s))
}

// Status of a user.
type Status string

// Statuses.
const (
	StatusActive  Status = "active"
	StatusBlocked Status = "blocked"
)

// User is a Quoker account.
type User struct {
	ID                int64
	Role              Role
	Status            Status
	TelegramUserID    int64 // 0 when Telegram is not linked
	TelegramUsername  string
	TelegramFirstName string
	CreatedAt         time.Time
	LastSeenAt        time.Time // zero if never seen
	TargetsCount      int
}

// TelegramLinked reports whether the user has linked Telegram.
func (u User) TelegramLinked() bool { return u.TelegramUserID != 0 }

// Principal is the authenticated caller of a request.
type Principal struct {
	SessionID int64
	UserID    int64
	Role      Role
}

// IsAdmin reports whether the caller is an administrator.
func (p Principal) IsAdmin() bool { return p.Role == RoleAdmin }

const (
	// DefaultInviteTTL is the invite lifetime unless the admin picks another.
	DefaultInviteTTL = 7 * 24 * time.Hour
	// sessionTouchInterval throttles last_seen_at writes.
	sessionTouchInterval = time.Minute
	// tokenBytes is the entropy of session tokens (docs/11-nfr-security.md).
	tokenBytes = 32
)

// Service implements account operations.
type Service struct {
	st   *store.Store
	now  func() time.Time
	rand io.Reader
}

// New returns a Service. now defaults to time.Now, rnd to crypto/rand.
func New(st *store.Store, now func() time.Time, rnd io.Reader) *Service {
	if now == nil {
		now = time.Now
	}
	if rnd == nil {
		rnd = rand.Reader
	}
	return &Service{st: st, now: now, rand: rnd}
}

func (s *Service) db() *sql.DB { return s.st.DB() }

// Registration is the input of Register.
type Registration struct {
	InviteCode string
	DeviceID   string
	DeviceName string
	AppVersion int64 // 0 if unknown
}

// Register redeems an invite: it creates the user with the invite's role and
// a session for the device, and returns the session token (shown once).
func (s *Service) Register(ctx context.Context, r Registration) (string, User, error) {
	code, ok := normalizeInviteCode(r.InviteCode)
	if !ok {
		return "", User{}, errs.New(errs.InvalidInvite, "malformed invite code")
	}
	token, tokenHash, err := s.newToken()
	if err != nil {
		return "", User{}, err
	}
	now := s.now().UnixMilli()
	var userID int64
	err = s.st.Tx(ctx, func(tx *sql.Tx) error {
		var (
			inviteID          int64
			role              string
			expiresAt         int64
			usedAt, revokedAt sql.NullInt64
		)
		err := tx.QueryRowContext(ctx,
			`SELECT id, role, expires_at, used_at, revoked_at FROM invites WHERE code_hash = ?`,
			hashInviteCode(code)).Scan(&inviteID, &role, &expiresAt, &usedAt, &revokedAt)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			return errs.New(errs.InvalidInvite, "unknown invite code")
		case err != nil:
			return fmt.Errorf("find invite: %w", err)
		case usedAt.Valid:
			return errs.New(errs.InvalidInvite, "invite already used")
		case revokedAt.Valid:
			return errs.New(errs.InvalidInvite, "invite revoked")
		case expiresAt <= now:
			return errs.New(errs.InvalidInvite, "invite expired")
		}

		res, err := tx.ExecContext(ctx,
			`INSERT INTO users (role, status, invite_id, created_at, last_seen_at) VALUES (?, 'active', ?, ?, ?)`,
			role, inviteID, now, now)
		if err != nil {
			return fmt.Errorf("insert user: %w", err)
		}
		if userID, err = res.LastInsertId(); err != nil {
			return fmt.Errorf("user id: %w", err)
		}
		res, err = tx.ExecContext(ctx,
			`UPDATE invites SET used_by = ?, used_at = ? WHERE id = ? AND used_at IS NULL`,
			userID, now, inviteID)
		if err != nil {
			return fmt.Errorf("redeem invite: %w", err)
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return errs.New(errs.InvalidInvite, "invite already used")
		}
		_, err = tx.ExecContext(ctx,
			`INSERT INTO sessions (user_id, token_hash, device_id, device_name, app_version, created_at, last_seen_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			userID, tokenHash, r.DeviceID, r.DeviceName, nullInt(r.AppVersion), now, now)
		if err != nil {
			return fmt.Errorf("insert session: %w", err)
		}
		return nil
	})
	if err != nil {
		return "", User{}, err
	}
	u, err := s.User(ctx, userID)
	if err != nil {
		return "", User{}, err
	}
	return token, u, nil
}

// Authenticate resolves a session token. A blocked user gets user_blocked
// even though the block revoked the session, so the app can show
// "access closed" instead of the onboarding.
func (s *Service) Authenticate(ctx context.Context, token string, appVersion int64) (Principal, error) {
	if raw, err := base64.RawURLEncoding.DecodeString(token); err != nil || len(raw) != tokenBytes {
		return Principal{}, errs.New(errs.Unauthorized, "malformed session token")
	}
	var (
		p          Principal
		role       string
		status     string
		revokedAt  sql.NullInt64
		lastSeenAt int64
	)
	err := s.db().QueryRowContext(ctx,
		`SELECT s.id, s.user_id, s.revoked_at, s.last_seen_at, u.role, u.status
		 FROM sessions s JOIN users u ON u.id = s.user_id
		 WHERE s.token_hash = ?`, hashToken(token)).
		Scan(&p.SessionID, &p.UserID, &revokedAt, &lastSeenAt, &role, &status)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Principal{}, errs.New(errs.Unauthorized, "unknown session")
	case err != nil:
		return Principal{}, fmt.Errorf("find session: %w", err)
	case Status(status) == StatusBlocked:
		return Principal{}, errs.New(errs.UserBlocked, "user is blocked")
	case revokedAt.Valid:
		return Principal{}, errs.New(errs.Unauthorized, "session revoked")
	}
	p.Role = Role(role)

	now := s.now()
	if now.Sub(time.UnixMilli(lastSeenAt)) >= sessionTouchInterval {
		if err := s.touch(ctx, p, now.UnixMilli(), appVersion); err != nil {
			return Principal{}, err
		}
	}
	return p, nil
}

func (s *Service) touch(ctx context.Context, p Principal, now, appVersion int64) error {
	return s.st.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`UPDATE sessions SET last_seen_at = ?, app_version = COALESCE(?, app_version) WHERE id = ?`,
			now, nullInt(appVersion), p.SessionID); err != nil {
			return fmt.Errorf("touch session: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE users SET last_seen_at = ? WHERE id = ?`, now, p.UserID); err != nil {
			return fmt.Errorf("touch user: %w", err)
		}
		return nil
	})
}

// Logout revokes a session permanently.
func (s *Service) Logout(ctx context.Context, sessionID int64) error {
	_, err := s.db().ExecContext(ctx,
		`UPDATE sessions SET revoked_at = ?, revoke_reason = 'logout' WHERE id = ? AND revoked_at IS NULL`,
		s.now().UnixMilli(), sessionID)
	if err != nil {
		return fmt.Errorf("logout: %w", err)
	}
	return nil
}

const userColumns = `u.id, u.role, u.status, u.tg_user_id, u.tg_username, u.tg_first_name, u.created_at, u.last_seen_at,
	(SELECT count(*) FROM targets t WHERE t.owner_user_id = u.id)`

func scanUser(sc interface{ Scan(...any) error }) (User, error) {
	var (
		u               User
		role, status    string
		tgID, lastSeen  sql.NullInt64
		username, first sql.NullString
		createdAt       int64
	)
	if err := sc.Scan(&u.ID, &role, &status, &tgID, &username, &first, &createdAt, &lastSeen, &u.TargetsCount); err != nil {
		return User{}, err
	}
	u.Role, u.Status = Role(role), Status(status)
	u.TelegramUserID, u.TelegramUsername, u.TelegramFirstName = tgID.Int64, username.String, first.String
	u.CreatedAt = msTime(createdAt)
	if lastSeen.Valid {
		u.LastSeenAt = msTime(lastSeen.Int64)
	}
	return u, nil
}

// User returns a user by id.
func (s *Service) User(ctx context.Context, id int64) (User, error) {
	u, err := scanUser(s.db().QueryRowContext(ctx, `SELECT `+userColumns+` FROM users u WHERE u.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, errs.New(errs.NotFound, "user not found")
	}
	if err != nil {
		return User{}, fmt.Errorf("get user: %w", err)
	}
	return u, nil
}

// ListUsers returns all users ordered by id.
func (s *Service) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.db().QueryContext(ctx, `SELECT `+userColumns+` FROM users u ORDER BY u.id`)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	return out, nil
}

// Block blocks a user and revokes all their sessions. Admins cannot block
// themselves.
func (s *Service) Block(ctx context.Context, actorID, userID int64) error {
	if actorID == userID {
		return errs.New(errs.Forbidden, "cannot block yourself")
	}
	now := s.now().UnixMilli()
	return s.st.Tx(ctx, func(tx *sql.Tx) error {
		if err := setStatus(ctx, tx, userID, StatusBlocked); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE sessions SET revoked_at = ?, revoke_reason = 'blocked' WHERE user_id = ? AND revoked_at IS NULL`,
			now, userID); err != nil {
			return fmt.Errorf("revoke sessions: %w", err)
		}
		return nil
	})
}

// Unblock unblocks a user and restores the sessions revoked by the block.
// Sessions ended by logout stay revoked.
func (s *Service) Unblock(ctx context.Context, userID int64) error {
	return s.st.Tx(ctx, func(tx *sql.Tx) error {
		if err := setStatus(ctx, tx, userID, StatusActive); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE sessions SET revoked_at = NULL, revoke_reason = NULL WHERE user_id = ? AND revoke_reason = 'blocked'`,
			userID); err != nil {
			return fmt.Errorf("restore sessions: %w", err)
		}
		return nil
	})
}

func setStatus(ctx context.Context, tx *sql.Tx, userID int64, st Status) error {
	res, err := tx.ExecContext(ctx, `UPDATE users SET status = ? WHERE id = ?`, string(st), userID)
	if err != nil {
		return fmt.Errorf("set user status: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errs.New(errs.NotFound, "user not found")
	}
	return nil
}

func (s *Service) newToken() (token string, hash []byte, err error) {
	b := make([]byte, tokenBytes)
	if _, err := io.ReadFull(s.rand, b); err != nil {
		return "", nil, fmt.Errorf("generate token: %w", err)
	}
	token = base64.RawURLEncoding.EncodeToString(b)
	return token, hashToken(token), nil
}

func hashToken(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}

func nullInt(v int64) sql.NullInt64 { return sql.NullInt64{Int64: v, Valid: v != 0} }

func msTime(ms int64) time.Time { return time.UnixMilli(ms).UTC() }
