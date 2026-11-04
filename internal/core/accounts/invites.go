package accounts

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/tuskar2321/quoker/internal/shared/errs"
)

// Invite codes have the form XXXX-XXXX over a 32-symbol alphabet without the
// look-alikes 0/O and 1/I: 40 bits, guarded by the registration rate limit,
// single use and expiry (docs/11-nfr-security.md).
const (
	inviteAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	inviteLen      = 8
	invitePrefix   = 4
	inviteAttempts = 3
)

// Invite is an issued invite. The code itself is never stored.
type Invite struct {
	ID         int64
	Role       Role
	CodePrefix string // first four characters, for the masked list
	CreatedAt  time.Time
	ExpiresAt  time.Time
	UsedBy     int64     // 0 if unused
	UsedAt     time.Time // zero if unused
}

// CreateInvite issues a single-use invite and returns its code, which is
// shown once. createdBy is 0 for the CLI bootstrap invite.
func (s *Service) CreateInvite(ctx context.Context, createdBy int64, role Role, ttl time.Duration) (string, time.Time, error) {
	if _, err := ParseRole(string(role)); err != nil {
		return "", time.Time{}, err
	}
	now := s.now()
	expires := now.Add(ttl)
	for range inviteAttempts {
		code, err := s.newInviteCode()
		if err != nil {
			return "", time.Time{}, err
		}
		_, err = s.db().ExecContext(ctx,
			`INSERT INTO invites (code_hash, code_prefix, role, created_by, created_at, expires_at)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			hashInviteCode(code), code[:invitePrefix], string(role), nullInt(createdBy),
			now.UnixMilli(), expires.UnixMilli())
		if err == nil {
			return formatInviteCode(code), msTime(expires.UnixMilli()), nil
		}
		if !strings.Contains(err.Error(), "UNIQUE") {
			return "", time.Time{}, fmt.Errorf("insert invite: %w", err)
		}
		// Hash collision with an existing invite: draw another code.
	}
	return "", time.Time{}, errors.New("insert invite: could not draw a unique code")
}

// CreateBootstrapInvite issues the first admin invite (`core admin
// bootstrap`). It refuses once an admin exists.
func (s *Service) CreateBootstrapInvite(ctx context.Context) (string, time.Time, error) {
	var admins int
	if err := s.db().QueryRowContext(ctx,
		`SELECT count(*) FROM users WHERE role = 'admin'`).Scan(&admins); err != nil {
		return "", time.Time{}, fmt.Errorf("count admins: %w", err)
	}
	if admins > 0 {
		return "", time.Time{}, errs.New(errs.Forbidden, "an admin already exists; create invites from the app or the bot")
	}
	return s.CreateInvite(ctx, 0, RoleAdmin, DefaultInviteTTL)
}

// ListInvites returns invites that were not revoked, newest first.
func (s *Service) ListInvites(ctx context.Context) ([]Invite, error) {
	rows, err := s.db().QueryContext(ctx,
		`SELECT id, role, code_prefix, created_at, expires_at, used_by, used_at
		 FROM invites WHERE revoked_at IS NULL ORDER BY created_at DESC, id DESC`)
	if err != nil {
		return nil, fmt.Errorf("list invites: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Invite
	for rows.Next() {
		var (
			inv              Invite
			role             string
			created, expires int64
			usedBy, usedAt   sql.NullInt64
		)
		if err := rows.Scan(&inv.ID, &role, &inv.CodePrefix, &created, &expires, &usedBy, &usedAt); err != nil {
			return nil, fmt.Errorf("scan invite: %w", err)
		}
		inv.Role = Role(role)
		inv.CreatedAt, inv.ExpiresAt = msTime(created), msTime(expires)
		inv.UsedBy = usedBy.Int64
		if usedAt.Valid {
			inv.UsedAt = msTime(usedAt.Int64)
		}
		out = append(out, inv)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list invites: %w", err)
	}
	return out, nil
}

// RevokeInvite revokes an unused invite. Revoking a used or already revoked
// invite is a no-op.
func (s *Service) RevokeInvite(ctx context.Context, id int64) error {
	res, err := s.db().ExecContext(ctx,
		`UPDATE invites SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL AND used_at IS NULL`,
		s.now().UnixMilli(), id)
	if err != nil {
		return fmt.Errorf("revoke invite: %w", err)
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return nil
	}
	var exists int
	err = s.db().QueryRowContext(ctx, `SELECT 1 FROM invites WHERE id = ?`, id).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return errs.New(errs.NotFound, "invite not found")
	}
	if err != nil {
		return fmt.Errorf("find invite: %w", err)
	}
	return nil
}

func (s *Service) newInviteCode() (string, error) {
	b := make([]byte, inviteLen)
	if _, err := io.ReadFull(s.rand, b); err != nil {
		return "", fmt.Errorf("generate invite code: %w", err)
	}
	for i := range b {
		b[i] = inviteAlphabet[b[i]%byte(len(inviteAlphabet))] // 256 % 32 == 0: unbiased
	}
	return string(b), nil
}

// normalizeInviteCode upper-cases the code and drops dashes and spaces; it
// returns the 8 characters or false if the input cannot be a code.
func normalizeInviteCode(in string) (string, bool) {
	var b strings.Builder
	for _, r := range strings.ToUpper(in) {
		switch {
		case r == '-' || r == ' ':
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			return "", false
		}
	}
	if b.Len() != inviteLen {
		return "", false
	}
	return b.String(), true
}

func formatInviteCode(code string) string { return code[:invitePrefix] + "-" + code[invitePrefix:] }

func hashInviteCode(normalized string) []byte {
	h := sha256.Sum256([]byte(normalized))
	return h[:]
}
