package httpapi

import (
	"net/http/httptest"
	"testing"

	"github.com/tuskar2321/quoker/internal/core/proxy"
)

// TestClientIP: X-Forwarded-For is believed only from a trusted proxy, and
// then the first untrusted hop from the right (REVIEW L7).
func TestClientIP(t *testing.T) {
	trusted := proxy.Default()
	tests := []struct {
		name, remote, xff, want string
	}{
		{"direct client", "203.0.113.5:1000", "", "203.0.113.5"},
		{"direct client forging the header", "203.0.113.5:1000", "1.2.3.4", "203.0.113.5"},
		{"through caddy", "172.18.0.2:1000", "198.51.100.7", "198.51.100.7"},
		{"through caddy, client-supplied prefix", "172.18.0.2:1000", "1.2.3.4, 198.51.100.7", "198.51.100.7"},
		{"through a chain of internal proxies", "172.18.0.2:1000", "198.51.100.7, 10.0.0.5", "198.51.100.7"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/v1/auth/register", nil) //nolint:noctx // test
			r.RemoteAddr = tt.remote
			if tt.xff != "" {
				r.Header.Set("X-Forwarded-For", tt.xff)
			}
			if got := clientIP(r, trusted); got != tt.want {
				t.Fatalf("clientIP = %q, want %q", got, tt.want)
			}
		})
	}
}
