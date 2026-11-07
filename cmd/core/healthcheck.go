package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"
)

const healthTimeout = 3 * time.Second

// healthcheck probes the local public API at http://127.0.0.1:<port>/healthz,
// the port taken from httpAddr (HTTP_ADDR). A nil error means healthy.
func healthcheck(httpAddr string) error {
	_, port, err := net.SplitHostPort(httpAddr)
	if err != nil {
		return fmt.Errorf("HTTP_ADDR %q: %w", httpAddr, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), healthTimeout)
	defer cancel()
	//nolint:gosec // the URL is always 127.0.0.1 with a port from our own config
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+net.JoinHostPort("127.0.0.1", port)+"/healthz", nil)
	if err != nil {
		return err
	}
	res, err := http.DefaultClient.Do(req) //nolint:gosec // see above
	if err != nil {
		return fmt.Errorf("healthcheck: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("healthcheck: status %d", res.StatusCode)
	}
	return nil
}
