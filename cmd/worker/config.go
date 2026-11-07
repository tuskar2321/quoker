package main

import (
	"errors"
	"fmt"
	"net"
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
	// CAFile is the project CA (CA_CERT). It is added to the system roots,
	// never replaces them: the core is served with a public certificate.
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
	// Transport is how the core is reached: "direct" or "reality" (through the
	// local xray tunnel). TRANSPORT, default direct. Only core traffic uses it.
	Transport string
	// CoreDialAddr is the TCP target for core traffic when Transport is
	// reality (CORE_DIAL_ADDR, e.g. xray:10443). TLS name stays CoreAddr's host.
	CoreDialAddr string
	// HealthAddr serves GET /healthz. HEALTH_ADDR, default 127.0.0.1:8090.
	HealthAddr string
	// LogLevel controls logging verbosity. LOG_LEVEL, default info.
	LogLevel string
}

const (
	transportDirect   = "direct"
	transportReality  = "reality"
	defaultHealthAddr = "127.0.0.1:8090"
)

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
		Transport:        or(getenv("TRANSPORT"), transportDirect),
		CoreDialAddr:     getenv("CORE_DIAL_ADDR"),
		HealthAddr:       or(getenv("HEALTH_ADDR"), defaultHealthAddr),
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
	switch c.Transport {
	case transportDirect:
	case transportReality:
		if c.CoreDialAddr == "" {
			errs = append(errs, errors.New("CORE_DIAL_ADDR is required when TRANSPORT=reality"))
		}
	default:
		errs = append(errs, fmt.Errorf("TRANSPORT %q: must be direct or reality", c.Transport))
	}
	if c.CoreDialAddr != "" {
		if _, _, err := net.SplitHostPort(c.CoreDialAddr); err != nil {
			errs = append(errs, fmt.Errorf("CORE_DIAL_ADDR %q: %w", c.CoreDialAddr, err))
		}
	}
	if _, _, err := net.SplitHostPort(c.HealthAddr); err != nil {
		errs = append(errs, fmt.Errorf("HEALTH_ADDR %q: %w", c.HealthAddr, err))
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
