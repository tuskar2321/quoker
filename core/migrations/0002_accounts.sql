-- Accounts additions. See docs/05-data-model.md.

-- Why a session was revoked: unblocking a user restores only the sessions
-- that the block revoked; a logout is permanent.
ALTER TABLE sessions ADD COLUMN revoke_reason TEXT CHECK (revoke_reason IN ('logout', 'blocked'));

-- First four characters of the invite code, shown masked in the admin list
-- ("K7Q2-••••"). The full code is never stored.
ALTER TABLE invites ADD COLUMN code_prefix TEXT NOT NULL DEFAULT '';
