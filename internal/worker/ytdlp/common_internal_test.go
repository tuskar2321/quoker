package ytdlp

import (
	"slices"
	"testing"
)

func TestCommonUserAgent(t *testing.T) {
	const ua = "Mozilla/5.0 test"
	tests := []struct {
		name   string
		ua     string
		url    string
		wantUA bool
	}{
		{"instagram gets the configured agent", ua, "https://www.instagram.com/reel/abc/", true},
		{"tiktok relies on impersonation", ua, "https://www.tiktok.com/@u/video/1", false},
		{"tiktok short link too", ua, "https://vt.tiktok.com/ZSabc/", false},
		{"lookalike host is not tiktok", ua, "https://nottiktok.com/x", true},
		{"no agent configured", "", "https://www.instagram.com/reel/abc/", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := New("yt-dlp", tt.ua, t.TempDir()).common(tt.url)
			got := slices.Contains(args, "--user-agent")
			if got != tt.wantUA {
				t.Fatalf("--user-agent present = %v, want %v (args %q)", got, tt.wantUA, args)
			}
			if got && !slices.Contains(args, tt.ua) {
				t.Fatalf("agent %q missing from %q", tt.ua, args)
			}
		})
	}
}
