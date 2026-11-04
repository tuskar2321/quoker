// Package config reads the core configuration from the environment.
// Variables are listed in docs/10-deploy.md; secrets live in the database.
package config

import (
	"errors"
	"fmt"
	"net"
	"strconv"
)

// Config is the core runtime configuration.
type Config struct {
	DBPath        string // DB_PATH, required
	HTTPAddr      string // HTTP_ADDR, public API, default :8080
	FilesAddr     string // FILES_ADDR, uploads from workers, default :8081
	GRPCAddr      string // GRPC_ADDR, worker link, default :9090
	CacheDir      string // CACHE_DIR, default ./cache
	CacheMaxBytes int64  // CACHE_MAX_BYTES, default 3 GiB
	// UploadBase is where workers PUT files: UPLOAD_BASE, or
	// https://w.<DOMAIN>/internal/v1 when only DOMAIN is set.
	UploadBase string
	LogLevel   string // LOG_LEVEL, default info
}

// Load reads the configuration using getenv (os.Getenv in production).
func Load(getenv func(string) string) (Config, error) {
	c := Config{
		DBPath:        getenv("DB_PATH"),
		HTTPAddr:      or(getenv("HTTP_ADDR"), ":8080"),
		FilesAddr:     or(getenv("FILES_ADDR"), ":8081"),
		GRPCAddr:      or(getenv("GRPC_ADDR"), ":9090"),
		CacheDir:      or(getenv("CACHE_DIR"), "./cache"),
		CacheMaxBytes: 3 << 30,
		UploadBase:    getenv("UPLOAD_BASE"),
		LogLevel:      or(getenv("LOG_LEVEL"), "info"),
	}
	if c.UploadBase == "" && getenv("DOMAIN") != "" {
		c.UploadBase = "https://w." + getenv("DOMAIN") + "/internal/v1"
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
