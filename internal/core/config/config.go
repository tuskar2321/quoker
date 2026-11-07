// Package config reads the core configuration from the environment.
// Variables are listed in docs/10-deploy.md; secrets live in the database.
package config

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/tuskar2321/quoker/internal/core/proxy"
)

// Config is the core runtime configuration.
type Config struct {
	DBPath        string // DB_PATH, required
	HTTPAddr      string // HTTP_ADDR, public API, default :8080
	FilesAddr     string // FILES_ADDR, uploads from workers, default :8081
	GRPCAddr      string // GRPC_ADDR, worker link, default :9090
	CacheDir      string // CACHE_DIR, default ./cache
	CacheMaxBytes int64  // CACHE_MAX_BYTES, default 3 GiB
	ReleasesDir   string // RELEASES_DIR, APKs, default ./releases
	// PublicURL prefixes apk_url: PUBLIC_URL, or https://api.<DOMAIN>.
	PublicURL string
	// UploadBase is where workers PUT files: UPLOAD_BASE, or
	// https://w.<DOMAIN>/internal/v1 when only DOMAIN is set.
	UploadBase string
	LogLevel   string // LOG_LEVEL, default info
	// BackupDir receives database backups: BACKUP_DIR. Empty disables the
	// daily backup; `core backup` then needs it set.
	BackupDir string
	// TrustedProxies are the CIDRs allowed to send X-Client-Cert-* and
	// X-Forwarded-For: TRUSTED_PROXIES, default loopback and private ranges.
	TrustedProxies string
}

// Load reads the configuration using getenv (os.Getenv in production).
func Load(getenv func(string) string) (Config, error) {
	c := Config{
		DBPath:         getenv("DB_PATH"),
		HTTPAddr:       or(getenv("HTTP_ADDR"), ":8080"),
		FilesAddr:      or(getenv("FILES_ADDR"), ":8081"),
		GRPCAddr:       or(getenv("GRPC_ADDR"), ":9090"),
		CacheDir:       or(getenv("CACHE_DIR"), "./cache"),
		CacheMaxBytes:  3 << 30,
		ReleasesDir:    or(getenv("RELEASES_DIR"), "./releases"),
		PublicURL:      strings.TrimRight(getenv("PUBLIC_URL"), "/"),
		UploadBase:     getenv("UPLOAD_BASE"),
		LogLevel:       or(getenv("LOG_LEVEL"), "info"),
		BackupDir:      getenv("BACKUP_DIR"),
		TrustedProxies: or(getenv("TRUSTED_PROXIES"), proxy.DefaultTrusted),
	}
	if d := getenv("DOMAIN"); d != "" {
		if c.UploadBase == "" {
			c.UploadBase = "https://w." + d + "/internal/v1"
		}
		if c.PublicURL == "" {
			c.PublicURL = "https://api." + d
		}
	}
	var errs []error
	if c.DBPath == "" {
		errs = append(errs, errors.New("DB_PATH is required"))
	}
	for name, addr := range map[string]string{"HTTP_ADDR": c.HTTPAddr, "FILES_ADDR": c.FilesAddr, "GRPC_ADDR": c.GRPCAddr} {
		if _, _, err := net.SplitHostPort(addr); err != nil {
			errs = append(errs, fmt.Errorf("%s %q: %w", name, addr, err))
		}
	}
	if _, err := proxy.Parse(c.TrustedProxies); err != nil {
		errs = append(errs, err)
	}
	if s := getenv("CACHE_MAX_BYTES"); s != "" {
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil || n <= 0 {
			errs = append(errs, fmt.Errorf("CACHE_MAX_BYTES %q: must be a positive integer", s))
		}
		c.CacheMaxBytes = n
	}
	if err := errors.Join(errs...); err != nil {
		return Config{}, fmt.Errorf("config: %w", err)
	}
	return c, nil
}

func or(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
