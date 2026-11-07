package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"
)

const (
	healthTimeout = 3 * time.Second
	dialTimeout   = 15 * time.Second
)

// coreDialer returns the TCP dialer for traffic to the core, or nil when the
// worker connects directly. With the reality transport every core connection
// goes to CORE_DIAL_ADDR (the local xray tunnel) while TLS server name, HTTP
// Host and gRPC authority keep coming from CORE_ADDR, so mTLS still ends at
// Caddy. Telegram and the platforms never use this dialer.
func coreDialer(cfg workerConfig) (func(ctx context.Context, addr string) (net.Conn, error), error) {
	if cfg.Transport != transportReality {
		return nil, nil
	}
	if cfg.CoreDialAddr == "" {
		return nil, errors.New("CORE_DIAL_ADDR is required when TRANSPORT=reality")
	}
	d := &net.Dialer{Timeout: dialTimeout, KeepAlive: 30 * time.Second}
	return func(ctx context.Context, _ string) (net.Conn, error) {
		return d.DialContext(ctx, "tcp", cfg.CoreDialAddr)
	}, nil
}

// withServerName clones c with ServerName set to the host of addr.
func withServerName(c *tls.Config, addr string) *tls.Config {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	out := c.Clone()
	out.ServerName = host
	return out
}

// startHealth serves GET /healthz (200 while the process runs) on addr.
// Close the returned server to stop it.
func startHealth(ctx context.Context, addr string, log *slog.Logger) (*http.Server, string, error) {
	lis, err := (&net.ListenConfig{}).Listen(ctx, "tcp", addr)
	if err != nil {
		return nil, "", fmt.Errorf("health listen: %w", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := srv.Serve(lis); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Warn("health server", "err", err)
		}
	}()
	log.Info("health listening", "addr", lis.Addr().String())
	return srv, lis.Addr().String(), nil
}

// healthcheck probes http://addr/healthz; a nil error means healthy.
func healthcheck(addr string) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("HEALTH_ADDR %q: %w", addr, err)
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	ctx, cancel := context.WithTimeout(context.Background(), healthTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+net.JoinHostPort(host, port)+"/healthz", nil)
	if err != nil {
		return err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("healthcheck: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("healthcheck: status %d", res.StatusCode)
	}
	return nil
}
