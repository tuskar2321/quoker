package main

import (
	"errors"
	"fmt"
	"strconv"
)

// workerConfig holds the worker runtime configuration.
// All values come from environment variables (docs/10-deploy.md).
type workerConfig struct {
	// WorkerID is the certificate CN; must match the DB record. WORKER_ID, required.
	WorkerID string
	// CoreAddr is the gRPC endpoint (host:port). CORE_ADDR, required.
	CoreAddr string
	// CertFile is the path to the mTLS client certificate. CERT_FILE, required in production.
	CertFile string
	// KeyFile is the path to the private key. KEY_FILE, required in production.
	KeyFile string
	// CAFile is the path to the CA certificate that signed the core's cert. CA_FILE, required in production.
	CAFile string
	// MaxParallel is the maximum number of concurrent tasks. MAX_PARALLEL, default 2.
	MaxParallel int
	// Version is the worker build version, injected at build time.
	Version string
	// YtdlpVersion is set after querying yt-dlp --version on startup.
	YtdlpVersion string
	// LogLevel controls logging verbosity. LOG_LEVEL, default info.
	LogLevel string
}

func loadWorkerConfig(getenv func(string) string, version string) (workerConfig, error) {
	c := workerConfig{
		WorkerID:    getenv("WORKER_ID"),
		CoreAddr:    getenv("CORE_ADDR"),
		CertFile:    getenv("CERT_FILE"),
		KeyFile:     getenv("KEY_FILE"),
		CAFile:      getenv("CA_FILE"),
		Version:     version,
		LogLevel:    or(getenv("LOG_LEVEL"), "info"),
		MaxParallel: 2,
	}
	if s := getenv("MAX_PARALLEL"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n <= 0 {
			return workerConfig{}, fmt.Errorf("config: MAX_PARALLEL %q: must be a positive integer", s)
		}
		c.MaxParallel = n
	}

	var errs []error
	if c.WorkerID == "" {
		errs = append(errs, errors.New("WORKER_ID is required"))
	}
	if c.CoreAddr == "" {
		errs = append(errs, errors.New("CORE_ADDR is required"))
	}
	if err := errors.Join(errs...); err != nil {
		return workerConfig{}, fmt.Errorf("config: %w", err)
	}
	return c, nil
}

func or(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
