package logging

import (
	"bytes"
	"strings"
	"testing"
)

// TestBaseLoggerHasNoComponent: subsystem loggers carry the key once
// (REVIEW L21).
func TestBaseLoggerHasNoComponent(t *testing.T) {
	var buf bytes.Buffer
	base, err := New(&buf, "info", "")
	if err != nil {
		t.Fatal(err)
	}
	base.With("component", "link").Info("hi")
	if n := strings.Count(buf.String(), `"component"`); n != 1 {
		t.Fatalf("record = %s", buf.String())
	}
}
