package settings

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/tuskar2321/quoker/internal/core/store"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "q.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return New(st.DB(), func() time.Time { return time.UnixMilli(1000) })
}

func TestGetSetInt(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	if _, ok, err := s.Get(ctx, BotUsername); err != nil || ok {
		t.Fatalf("unset Get: ok=%v err=%v", ok, err)
	}
	if n, err := s.Int(ctx, MinSupportedVersionCode, 7); err != nil || n != 7 {
		t.Fatalf("unset Int = %d, %v; want default 7", n, err)
	}
	for _, v := range []string{"12", "15"} { // second write updates
		if err := s.Set(ctx, MinSupportedVersionCode, v); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := s.Int(ctx, MinSupportedVersionCode, 0); err != nil || n != 15 {
		t.Fatalf("Int = %d, %v; want 15", n, err)
	}
	if err := s.Set(ctx, BotUsername, "quoker_bot"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Int(ctx, BotUsername, 0); err == nil {
		t.Fatal("Int on a non-integer value must fail")
	}
}

func TestParseKey(t *testing.T) {
	tests := []struct {
		in      string
		wantErr bool
	}{
		{"bot_token", false},
		{"min_supported_version_code", false},
		{"BOT_TOKEN", true},
		{"password", true},
	}
	for _, tt := range tests {
		if _, err := ParseKey(tt.in); (err != nil) != tt.wantErr {
			t.Errorf("ParseKey(%q) err = %v, wantErr %v", tt.in, err, tt.wantErr)
		}
	}
}
