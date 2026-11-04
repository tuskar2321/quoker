// Package proxy decides which peers may vouch for a request: the core
// trusts X-Client-Cert-* and X-Forwarded-For only from Caddy
// (docs/10-deploy.md), which reaches it over the private docker network.
package proxy

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// DefaultTrusted are loopback and private ranges: the docker network Caddy
// talks to the core from, and local development.
const DefaultTrusted = "127.0.0.0/8,::1/128,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,fc00::/7"

// Trusted is a set of networks.
type Trusted struct{ prefixes []netip.Prefix }

// Parse reads comma-separated CIDRs; an empty string means DefaultTrusted.
func Parse(s string) (Trusted, error) {
	if strings.TrimSpace(s) == "" {
		s = DefaultTrusted
	}
	var t Trusted
	for _, part := range strings.Split(s, ",") {
		p, err := netip.ParsePrefix(strings.TrimSpace(part))
		if err != nil {
			return Trusted{}, fmt.Errorf("trusted proxy %q: %w", part, err)
		}
		t.prefixes = append(t.prefixes, p.Masked())
	}
	return t, nil
}

// Default returns DefaultTrusted.
func Default() Trusted {
	t, _ := Parse(DefaultTrusted)
	return t
}

// Contains reports whether addr ("ip" or "ip:port") is trusted.
func (t Trusted) Contains(addr string) bool {
	host := addr
	if h, _, err := net.SplitHostPort(addr); err == nil {
		host = h
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	ip = ip.Unmap()
	for _, p := range t.prefixes {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

// Only serves h to trusted peers and answers 403 to everyone else.
func Only(t Trusted, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !t.Contains(r.RemoteAddr) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		h.ServeHTTP(w, r)
	})
}
