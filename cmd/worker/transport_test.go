package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestLoadWorkerConfigTransport(t *testing.T) {
	base := map[string]string{"WORKER_ID": "w1", "CORE_ADDR": "w.example.com:443"}
	tests := []struct {
		name    string
		extra   map[string]string
		wantErr string
		check   func(*testing.T, workerConfig)
	}{
		{name: "defaults", check: func(t *testing.T, c workerConfig) {
			if c.Transport != "direct" || c.HealthAddr != "127.0.0.1:8090" {
				t.Errorf("got %q %q", c.Transport, c.HealthAddr)
			}
		}},
		{name: "reality ok", extra: map[string]string{"TRANSPORT": "reality", "CORE_DIAL_ADDR": "xray:10443"}},
		{name: "reality without dial addr", extra: map[string]string{"TRANSPORT": "reality"}, wantErr: "CORE_DIAL_ADDR"},
		{name: "unknown transport", extra: map[string]string{"TRANSPORT": "wireguard"}, wantErr: "TRANSPORT"},
		{name: "bad dial addr", extra: map[string]string{"TRANSPORT": "reality", "CORE_DIAL_ADDR": "xray"}, wantErr: "CORE_DIAL_ADDR"},
		{name: "bad health addr", extra: map[string]string{"HEALTH_ADDR": "nope"}, wantErr: "HEALTH_ADDR"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := map[string]string{}
			for k, v := range base {
				env[k] = v
			}
			for k, v := range tt.extra {
				env[k] = v
			}
			c, err := loadWorkerConfig(func(k string) string { return env[k] }, "dev")
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tt.check != nil {
				tt.check(t, c)
			}
		})
	}
}

// TestRealityUploadTransport: HTTPS goes to CORE_DIAL_ADDR, TLS name and Host
// stay those of the core address.
func TestRealityUploadTransport(t *testing.T) {
	var mu sync.Mutex
	var sni, host string
	ts := httptest.NewUnstartedServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		mu.Lock()
		host = r.Host
		mu.Unlock()
	}))
	ts.StartTLS()
	t.Cleanup(ts.Close)
	ts.TLS.GetConfigForClient = func(h *tls.ClientHelloInfo) (*tls.Config, error) {
		mu.Lock()
		sni = h.ServerName
		mu.Unlock()
		return nil, nil
	}
	roots := x509.NewCertPool()
	roots.AddCert(ts.Certificate())

	cfg := workerConfig{Transport: transportReality, CoreAddr: "example.com:443", CoreDialAddr: ts.Listener.Addr().String()}
	dial, err := coreDialer(cfg)
	if err != nil || dial == nil {
		t.Fatalf("coreDialer = %v, %v", dial != nil, err)
	}
	tr := &http.Transport{
		TLSClientConfig: withServerName(&tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS13}, cfg.CoreAddr),
		DialContext: func(ctx context.Context, _, addr string) (net.Conn, error) {
			return dial(ctx, addr)
		},
	}
	t.Cleanup(tr.CloseIdleConnections)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.com:443/x", nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := (&http.Client{Transport: tr}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	mu.Lock()
	defer mu.Unlock()
	if sni != "example.com" || host != "example.com:443" {
		t.Errorf("sni = %q host = %q", sni, host)
	}
}

func TestCoreDialerDirect(t *testing.T) {
	d, err := coreDialer(workerConfig{Transport: transportDirect})
	if d != nil || err != nil {
		t.Errorf("direct: %v %v", d != nil, err)
	}
	if _, err := coreDialer(workerConfig{Transport: transportReality}); err == nil {
		t.Error("reality without CORE_DIAL_ADDR must fail")
	}
}

func TestHealth(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv, addr, err := startHealth(context.Background(), "127.0.0.1:0", log)
	if err != nil {
		t.Fatal(err)
	}
	if err := healthcheck(addr); err != nil {
		t.Errorf("healthy worker: %v", err)
	}
	_ = srv.Close()
	if err := healthcheck(addr); err == nil {
		t.Error("healthcheck after close must fail")
	}
}
