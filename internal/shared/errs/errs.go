// Package errs defines the typed domain error codes shared by core and
// worker. Public codes are listed in docs/03-public-api.md, worker codes in
// docs/04-core-worker-protocol.md.
package errs

import (
	"errors"
	"fmt"
)

// Code is a stable machine-readable error code.
type Code string

// Public API codes.
const (
	Unauthorized       Code = "unauthorized"
	Forbidden          Code = "forbidden"
	UserBlocked        Code = "user_blocked"
	UpgradeRequired    Code = "upgrade_required"
	InvalidRequest     Code = "invalid_request"
	InvalidInvite      Code = "invalid_invite"
	TelegramNotLinked  Code = "telegram_not_linked"
	LinkTokenExpired   Code = "link_token_expired" //nolint:gosec // G101: an error code, not a credential
	UnsupportedURL     Code = "unsupported_url"
	UnsupportedContent Code = "unsupported_content"
	PrivateVideo       Code = "private_video"
	NotFound           Code = "not_found"
	FileNotReady       Code = "file_not_ready"
	TooLarge           Code = "too_large"
	TargetInactive     Code = "target_inactive"
	TargetForbidden    Code = "target_forbidden"
	RateLimited        Code = "rate_limited"
	WorkerUnavailable  Code = "worker_unavailable"
	UpstreamError      Code = "upstream_error"
	Internal           Code = "internal"
)

// Worker-only codes.
const (
	LoginRequired Code = "login_required"
	Cancelled     Code = "cancelled"
)

// Retryable reports whether an operation failing with code may succeed if
// repeated unchanged.
func (c Code) Retryable() bool {
	switch c {
	case RateLimited, UpstreamError, WorkerUnavailable, Internal:
		return true
	}
	return false
}

// Error is a domain error. Detail is an English diagnostic for logs and is
// never shown to users.
type Error struct {
	Code   Code
	Detail string
	Err    error
}

func (e *Error) Error() string {
	switch {
	case e.Detail != "" && e.Err != nil:
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Detail, e.Err)
	case e.Detail != "":
		return fmt.Sprintf("%s: %s", e.Code, e.Detail)
	case e.Err != nil:
		return fmt.Sprintf("%s: %v", e.Code, e.Err)
	}
	return string(e.Code)
}

func (e *Error) Unwrap() error { return e.Err }

// New returns an error with code and diagnostic detail.
func New(code Code, detail string) *Error { return &Error{Code: code, Detail: detail} }

// Wrap returns an error with code wrapping err.
func Wrap(code Code, err error) *Error { return &Error{Code: code, Err: err} }

// CodeOf returns the code of the first *Error in err's chain, or Internal.
func CodeOf(err error) Code {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return Internal
}

// Is reports whether err carries code.
func Is(err error, code Code) bool {
	var e *Error
	return errors.As(err, &e) && e.Code == code
}
