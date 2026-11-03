// Package config reads the core configuration from the environment.
// Variables are listed in docs/10-deploy.md; secrets live in the database.
package config

import (
	"errors"
	"fmt"
	"net"
)

// Config is the core runtime configuration.
type Config struct {
	DBPath   string // DB_PATH, required
	HTTPAddr string // HTTP_ADDR, public API, default :8080
	LogLevel string // LOG_LEVEL, default info
}

// Load reads the configuration using getenv (os.Getenv in production).
func Load(getenv func(string) string) (Config, error) {
	c := Config{
		DBPath:   getenv("DB_PATH"),
		HTTPAddr: or(getenv("HTTP_ADDR"), ":8080"),
		LogLevel: or(getenv("LOG_LEVEL"), "info"),
	}
	var errs []error
	if c.DBPath == "" {
		errs = append(errs, errors.New("DB_PATH is required"))
	}
	if _, _, err := net.SplitHostPort(c.HTTPAddr); err != nil {
		errs = append(errs, fmt.Errorf("HTTP_ADDR %q: %w", c.HTTPAddr, err))
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
