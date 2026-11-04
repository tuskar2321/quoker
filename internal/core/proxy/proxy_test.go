package proxy_test

import (
	"testing"

	"github.com/tuskar2321/quoker/internal/core/proxy"
)

func TestContains(t *testing.T) {
	def := proxy.Default()
	for addr, want := range map[string]bool{
		"127.0.0.1:5000":        true,
		"172.18.0.3:41000":      true, // docker network
		"[::1]:80":              true,
		"[::ffff:10.1.2.3]:443": true,
		"203.0.113.7:5000":      false, // a stranger on the public interface
		"8.8.8.8":               false,
		"not an address":        false,
	} {
		if got := def.Contains(addr); got != want {
			t.Errorf("Contains(%q) = %v, want %v", addr, got, want)
		}
	}
	custom, err := proxy.Parse("10.9.0.0/16")
	if err != nil || custom.Contains("127.0.0.1") || !custom.Contains("10.9.1.1:1") {
		t.Fatalf("custom = %v, %v", custom, err)
	}
	if _, err := proxy.Parse("10.0.0.0/33"); err == nil {
		t.Fatal("bad prefix accepted")
	}
}
