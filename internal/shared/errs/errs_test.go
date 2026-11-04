package errs

import (
	"errors"
	"fmt"
	"testing"
)

func TestCodeOf(t *testing.T) {
	base := errors.New("boom")
	tests := []struct {
		name string
		err  error
		want Code
	}{
		{"plain error", base, Internal},
		{"domain error", New(InvalidInvite, "expired"), InvalidInvite},
		{"wrapped domain error", fmt.Errorf("register: %w", New(Forbidden, "x")), Forbidden},
		{"wrap keeps cause", Wrap(UpstreamError, base), UpstreamError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CodeOf(tt.err); got != tt.want {
				t.Fatalf("CodeOf = %s, want %s", got, tt.want)
			}
			if !Is(tt.err, tt.want) && tt.want != Internal {
				t.Fatalf("Is(%s) = false", tt.want)
			}
		})
	}
	if !errors.Is(Wrap(UpstreamError, base), base) {
		t.Fatal("Wrap must keep the cause in the chain")
	}
}

func TestRetryable(t *testing.T) {
	tests := []struct {
		code Code
		want bool
	}{
		{RateLimited, true}, {UpstreamError, true}, {WorkerUnavailable, true}, {Internal, true},
		{InvalidInvite, false}, {PrivateVideo, false}, {Cancelled, false}, {LoginRequired, false},
	}
	for _, tt := range tests {
		if got := tt.code.Retryable(); got != tt.want {
			t.Errorf("%s.Retryable() = %v, want %v", tt.code, got, tt.want)
		}
	}
}
