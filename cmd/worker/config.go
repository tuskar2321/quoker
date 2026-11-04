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
	// CertFile is the path to the mTLS client certificate. CLIENT_CERT, required in production.
	CertFile string
	// KeyFile is the path to the private key. CLIENT_KEY, required in production.
	KeyFile string
	// CAFile is the path to the CA certificate that signed the core's cert. CA_CERT, required in production.
	CAFile string
	// YtdlpBin is the yt-dlp executable. YTDLP_BIN, default yt-dlp.
	YtdlpBin string
	// TmpDir holds the file cache and per-task scratch space. TMP_DIR, default /tmp/quoker.
	TmpDir string
	// TmpCacheMaxBytes bounds the file cache. TMP_CACHE_MAX_BYTES, default 1 GiB.
	TmpCacheMaxBytes int64
	// UserAgent is sent to the platforms. USER_AGENT, default a current mobile UA.
	UserAgent string
	// MaxParallel is the maximum number of concurrent tasks. MAX_PARALLEL, default 2.
	MaxParallel int
	// Version is the worker build version, injected at build time.
	Version string
	// YtdlpVersion is set after querying yt-dlp --version on startup.
	YtdlpVersion string
	// LogLevel controls logging verbosity. LOG_LEVEL, default info.
	LogLevel string
}

// defaultUserAgent is a current mobile Chrome; platforms serve mobile-friendly
// formats to it.
const defaultUserAgent = "Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Mobile Safari/537.36"

func loadWorkerConfig(getenv func(string) string, version string) (workerConfig, error) {
	c := workerConfig{
		WorkerID:         getenv("WORKER_ID"),
		CoreAddr:         getenv("CORE_ADDR"),
		CertFile:         getenv("CLIENT_CERT"),
		KeyFile:          getenv("CLIENT_KEY"),
		CAFile:           getenv("CA_CERT"),
		YtdlpBin:         or(getenv("YTDLP_BIN"), "yt-dlp"),
		TmpDir:           or(getenv("TMP_DIR"), "/tmp/quoker"),
		TmpCacheMaxBytes: 1 << 30,
		UserAgent:        or(getenv("USER_AGENT"), defaultUserAgent),
		Version:          version,
		LogLevel:         or(getenv("LOG_LEVEL"), "info"),
		MaxParallel:      2,
	}
	if s := getenv("TMP_CACHE_MAX_BYTES"); s != "" {
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil || n <= 0 {
			return workerConfig{}, fmt.Errorf("config: TMP_CACHE_MAX_BYTES %q: must be a positive integer", s)
		}
		c.TmpCacheMaxBytes = n
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
