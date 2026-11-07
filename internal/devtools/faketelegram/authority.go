package faketelegram

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"time"
)

const (
	caCertFile = "ca.crt"
	caKeyFile  = "ca.key"
	leafValid  = 365 * 24 * time.Hour
	caValid    = 10 * 365 * 24 * time.Hour
)

// Authority is the throwaway CA that signs the certificate the fake serves for
// the Bot API host. Workers and the core trust it through a CA bundle, so the
// real Bot API host can be pointed at the fake without changing any code.
// The key lives only in the dev volume; it is never part of the repository.
type Authority struct {
	cert    *x509.Certificate
	key     *ecdsa.PrivateKey
	certPEM []byte
}

// LoadAuthority reads the CA from dir, creating it when dir has none. The same
// CA is reused across restarts, so clients that already trust it keep working.
func LoadAuthority(dir string, now time.Time) (*Authority, error) {
	certPEM, certErr := os.ReadFile(filepath.Join(dir, caCertFile)) //nolint:gosec // operator-supplied dev directory
	keyPEM, keyErr := os.ReadFile(filepath.Join(dir, caKeyFile))    //nolint:gosec // operator-supplied dev directory
	if certErr == nil && keyErr == nil {
		return parseAuthority(certPEM, keyPEM)
	}
	// Only a missing pair means "create": an unreadable file must not be
	// silently replaced with a new CA that every client would reject.
	for _, err := range []error{certErr, keyErr} {
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("read CA files: %w", err)
		}
	}
	a, err := NewAuthority(now)
	if err != nil {
		return nil, err
	}
	keyPEM, err = encodeKey(a.key)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil { //nolint:gosec // shared with the worker container, read-only there
		return nil, fmt.Errorf("create CA dir: %w", err)
	}
	if err := writeAtomic(filepath.Join(dir, caKeyFile), keyPEM, 0o600); err != nil {
		return nil, err
	}
	if err := writeAtomic(filepath.Join(dir, caCertFile), a.certPEM, 0o644); err != nil { //nolint:gosec // public certificate
		return nil, err
	}
	return a, nil
}

// NewAuthority creates a new CA that is valid for ten years from now.
func NewAuthority(now time.Time) (*Authority, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate CA key: %w", err)
	}
	serial, err := randomSerial()
	if err != nil {
		return nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "Quoker dev fake Telegram CA", Organization: []string{"Quoker dev"}},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(caValid),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, fmt.Errorf("create CA certificate: %w", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, fmt.Errorf("parse CA certificate: %w", err)
	}
	return &Authority{cert: cert, key: key, certPEM: pemEncode("CERTIFICATE", der)}, nil
}

func parseAuthority(certPEM, keyPEM []byte) (*Authority, error) {
	cb, _ := pem.Decode(certPEM)
	if cb == nil {
		return nil, errors.New("CA certificate is not PEM")
	}
	cert, err := x509.ParseCertificate(cb.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse CA certificate: %w", err)
	}
	kb, _ := pem.Decode(keyPEM)
	if kb == nil {
		return nil, errors.New("CA key is not PEM")
	}
	key, err := x509.ParseECPrivateKey(kb.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse CA key: %w", err)
	}
	return &Authority{cert: cert, key: key, certPEM: pemEncode("CERTIFICATE", cb.Bytes)}, nil
}

// CertPEM returns the CA certificate in PEM.
func (a *Authority) CertPEM() []byte {
	return append([]byte(nil), a.certPEM...)
}

// ServerTLSConfig returns a TLS configuration with a new leaf certificate for
// host, signed by the CA and valid for one year from now.
func (a *Authority) ServerTLSConfig(host string, now time.Time) (*tls.Config, error) {
	if host == "" {
		return nil, errors.New("TLS host is empty")
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate leaf key: %w", err)
	}
	serial, err := randomSerial()
	if err != nil {
		return nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: host},
		DNSNames:     []string{host},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.Add(leafValid),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, a.cert, &key.PublicKey, a.key)
	if err != nil {
		return nil, fmt.Errorf("create leaf certificate: %w", err)
	}
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		Certificates: []tls.Certificate{{
			Certificate: [][]byte{der, a.cert.Raw},
			PrivateKey:  key,
		}},
	}, nil
}

// WriteBundle writes a CA bundle to path: the system roots from systemRoots
// (skipped when the file does not exist) followed by the CA certificate. A
// client that reads it through SSL_CERT_FILE keeps trusting public CAs.
func (a *Authority) WriteBundle(path, systemRoots string) error {
	var bundle []byte
	if systemRoots != "" {
		b, err := os.ReadFile(systemRoots) //nolint:gosec // operator-supplied path
		switch {
		case err == nil:
			bundle = append(bundle, b...)
			if len(b) > 0 && b[len(b)-1] != '\n' {
				bundle = append(bundle, '\n')
			}
		case !errors.Is(err, os.ErrNotExist):
			return fmt.Errorf("read system roots: %w", err)
		}
	}
	bundle = append(bundle, a.certPEM...)
	return writeAtomic(path, bundle, 0o644) //nolint:gosec // public certificates, read by the worker container
}

func encodeKey(key *ecdsa.PrivateKey) ([]byte, error) {
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("marshal CA key: %w", err)
	}
	return pemEncode("EC PRIVATE KEY", der), nil
}

func pemEncode(typ string, der []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: der})
}

func randomSerial() (*big.Int, error) {
	n, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	if err != nil {
		return nil, fmt.Errorf("serial number: %w", err)
	}
	return n.Add(n, big.NewInt(1)), nil
}

// writeAtomic replaces path with data, so readers never see a partial file.
func writeAtomic(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }() // no-op after a successful rename
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		return fmt.Errorf("chmod %s: %w", path, err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}
