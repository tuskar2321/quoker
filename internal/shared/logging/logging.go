// Package logging builds the JSON slog logger shared by core and worker.
// Field names are listed in docs/11-nfr-security.md.
package logging

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
)

// New returns a JSON logger writing to w at the given level
// (debug, info, warn, error; empty means info) tagged with component.
func New(w io.Writer, level, component string) (*slog.Logger, error) {
	var l slog.Level
	if err := l.UnmarshalText([]byte(strings.TrimSpace(level))); err != nil && level != "" {
		return nil, fmt.Errorf("invalid log level %q: %w", level, err)
	}
	h := slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level: l,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if len(groups) == 0 && a.Key == slog.TimeKey {
				a.Key = "ts"
			}
			return a
		},
	})
	return slog.New(h).With("component", component), nil
}
