package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// projectCA writes a self-signed CA like the one that signs worker certs.
func projectCA(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "quoker project CA"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "ca.crt")
	if err := os.WriteFile(p, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestServerRootsKeepPublicCAs: with CA_CERT set as in docs/10, a core
// certificate from a public CA (here: the test server's, standing in for
// Let's Encrypt among the "system" roots) must still verify.
func TestServerRootsKeepPublicCAs(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()
	public := x509.NewCertPool()
	public.AddCert(srv.Certificate())

	roots, err := serverRoots(projectCA(t), func() (*x509.CertPool, error) { return public, nil })
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}}}
	res, err := client.Get(srv.URL) //nolint:noctx // test
	if err != nil {
		t.Fatalf("public core certificate rejected: %v", err)
	}
	_ = res.Body.Close()

	if _, err := serverRoots(filepath.Join(t.TempDir(), "missing.crt"), x509.SystemCertPool); err == nil {
		t.Fatal("missing CA file accepted")
	}
}
