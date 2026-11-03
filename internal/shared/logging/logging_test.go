package logging

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestNew(t *testing.T) {
	tests := []struct {
		level     string
		wantErr   bool
		debugSeen bool
	}{
		{level: "", debugSeen: false},
		{level: "info", debugSeen: false},
		{level: "DEBUG", debugSeen: true},
		{level: "warn", debugSeen: false},
		{level: "loud", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.level, func(t *testing.T) {
			var buf bytes.Buffer
			log, err := New(&buf, tt.level, "core")
			if tt.wantErr {
				if err == nil {
					t.Fatal("want error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			log.Debug("d")
			if got := buf.Len() > 0; got != tt.debugSeen {
				t.Fatalf("debug written = %v, want %v", got, tt.debugSeen)
			}
			buf.Reset()
			log.Error("e")
			var rec map[string]any
			if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
				t.Fatalf("not JSON: %q", buf.String())
			}
			if rec["component"] != "core" || rec["msg"] != "e" || rec["ts"] == nil {
				t.Fatalf("record = %v", rec)
			}
		})
	}
}
