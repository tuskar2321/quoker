package faketelegram_test

import (
	"crypto/x509"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tuskar2321/quoker/internal/devtools/faketelegram"
)

func TestAuthorityLeafVerification(t *testing.T) {
	now := time.Now()
	a, err := faketelegram.NewAuthority(now)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(a.CertPEM()) {
		t.Fatal("CA PEM does not parse")
	}
	cfg, err := a.ServerTLSConfig("api.telegram.org", now)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(cfg.Certificates[0].Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		host   string
		wantOK bool
	}{
		{"Bot API host verifies", "api.telegram.org", true},
		{"other host is rejected", "example.com", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := leaf.Verify(x509.VerifyOptions{DNSName: tt.host, Roots: roots, CurrentTime: now})
			if (err == nil) != tt.wantOK {
				t.Fatalf("verify %s: err = %v, want ok=%v", tt.host, err, tt.wantOK)
			}
		})
	}
}

func TestLoadAuthorityReusesCA(t *testing.T) {
	dir := t.TempDir()
	first, err := faketelegram.LoadAuthority(dir, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	second, err := faketelegram.LoadAuthority(dir, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if string(first.CertPEM()) != string(second.CertPEM()) {
		t.Fatal("second start created a new CA; clients that trust the first would break")
	}
	info, err := os.Stat(filepath.Join(dir, "ca.key"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("ca.key mode = %v, want 0600", info.Mode().Perm())
	}
}

func TestLoadAuthorityRejectsBrokenFiles(t *testing.T) {
	tests := []struct {
		name    string
		crt     string
		key     string
		wantErr bool
	}{
		{"garbage certificate", "not pem", "not pem", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "ca.crt"), []byte(tt.crt), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "ca.key"), []byte(tt.key), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := faketelegram.LoadAuthority(dir, time.Now())
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, want error=%v", err, tt.wantErr)
			}
		})
	}
}

func TestWriteBundleKeepsSystemRoots(t *testing.T) {
	tests := []struct {
		name       string
		systemRoot string // "" means no system roots file
		missing    bool
	}{
		{"system roots are prepended", "-----BEGIN CERTIFICATE-----\nSYSTEM\n-----END CERTIFICATE-----", false},
		{"missing system roots are skipped", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			a, err := faketelegram.NewAuthority(time.Now())
			if err != nil {
				t.Fatal(err)
			}
			sys := filepath.Join(dir, "system.pem")
			if !tt.missing {
				if err := os.WriteFile(sys, []byte(tt.systemRoot), 0o600); err != nil {
					t.Fatal(err)
				}
			} else {
				sys = filepath.Join(dir, "absent.pem")
			}
			out := filepath.Join(dir, "bundle.pem")
			if err := a.WriteBundle(out, sys); err != nil {
				t.Fatal(err)
			}
			b, err := os.ReadFile(out) //nolint:gosec // test-owned path
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(b), "BEGIN CERTIFICATE") {
				t.Fatalf("bundle has no certificate: %q", b)
			}
			if got := strings.Contains(string(b), "SYSTEM"); got == tt.missing {
				t.Fatalf("system roots present = %v, want %v", got, !tt.missing)
			}
			if !strings.Contains(string(b), string(a.CertPEM())) {
				t.Fatal("bundle does not contain the CA")
			}
		})
	}
}
