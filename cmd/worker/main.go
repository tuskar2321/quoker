// Command worker runs the Quoker worker: downloads videos, sends to Telegram.
// See docs/02-architecture.md (worker components).
//
// Usage:
//
//	worker [serve]   connect to core and process tasks
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	linkv1 "github.com/tuskar2321/quoker/gen/quoker/link/v1"
	"github.com/tuskar2321/quoker/internal/shared/logging"
	workerlink "github.com/tuskar2321/quoker/internal/worker/link"
	"github.com/tuskar2321/quoker/internal/worker/runner"
	"github.com/tuskar2321/quoker/internal/worker/tasks"
	"github.com/tuskar2321/quoker/internal/worker/telegram"
	"github.com/tuskar2321/quoker/internal/worker/tmpcache"
	"github.com/tuskar2321/quoker/internal/worker/upload"
	"github.com/tuskar2321/quoker/internal/worker/ytdlp"
)

// version is set at build time: -ldflags "-X main.version=<tag>".
var version = "dev"

func main() {
	if err := run(os.Args[1:], os.Getenv); err != nil {
		fmt.Fprintln(os.Stderr, "worker:", err)
		os.Exit(1)
	}
}

func run(args []string, getenv func(string) string) error {
	if len(args) > 0 && args[0] != "serve" {
		return fmt.Errorf("usage: worker [serve]")
	}
	cfg, err := loadWorkerConfig(getenv, version)
	if err != nil {
		return err
	}

	log, err := logging.New(os.Stdout, cfg.LogLevel, "worker")
	if err != nil {
		return err
	}

	tlsCfg, err := buildTLS(cfg)
	if err != nil {
		return fmt.Errorf("tls: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := os.MkdirAll(filepath.Join(cfg.TmpDir, "work"), 0o700); err != nil {
		return fmt.Errorf("tmp dir: %w", err)
	}
	ytdl := ytdlp.New(cfg.YtdlpBin, cfg.UserAgent, filepath.Join(cfg.TmpDir, "work"))
	if v, err := ytdl.Version(ctx); err != nil {
		log.Warn("yt-dlp unavailable, video tasks will fail", "bin", cfg.YtdlpBin, "err", err)
	} else {
		cfg.YtdlpVersion = v
	}
	cache, err := tmpcache.New(filepath.Join(cfg.TmpDir, "files"), cfg.TmpCacheMaxBytes)
	if err != nil {
		return err
	}
	// Uploads to the core carry the client certificate; Telegram and the
	// platforms are reached with the default transport.
	coreHTTP := &http.Client{Transport: &http.Transport{TLSClientConfig: tlsCfg, ForceAttemptHTTP2: true}}
	handlers := tasks.New(tasks.Deps{
		YtDlp:     ytdl,
		Telegram:  telegram.New(&http.Client{}),
		Upload:    upload.New(coreHTTP),
		Cache:     cache,
		UserAgent: cfg.UserAgent,
		Log:       log.With("component", "tasks"),
	})

	// configProxy forwards CoreConfig calls to the link client once it exists.
	// This breaks the circular dependency: runner needs the client's config,
	// client needs the runner as its task handler.
	proxy := &configProxy{}

	taskRunner := runner.New(handlers.Runner(), proxy, cfg.MaxParallel, log.With("component", "runner"))

	client := workerlink.New(workerlink.Config{
		WorkerID:     cfg.WorkerID,
		Version:      cfg.Version,
		YtdlpVersion: cfg.YtdlpVersion,
		MaxParallel:  int32(cfg.MaxParallel), //nolint:gosec // small positive value
		CoreAddr:     cfg.CoreAddr,
		TLSConfig:    tlsCfg,
		Handler:      taskRunner,
	}, log, nil)
	proxy.client = client

	log.Info("starting worker", "id", cfg.WorkerID, "core", cfg.CoreAddr, "version", cfg.Version, "ytdlp", cfg.YtdlpVersion)
	client.Run(ctx)
	log.Info("worker stopped")
	return nil
}

// buildTLS creates a mTLS config from the cert/key/CA files.
// Returns nil when no cert files are configured (dev/test: insecure transport).
func buildTLS(cfg workerConfig) (*tls.Config, error) {
	if cfg.CertFile == "" && cfg.KeyFile == "" {
		return nil, nil
	}
	cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("load cert: %w", err)
	}
	tlsCfg := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS13,
	}
	if cfg.CAFile != "" {
		ca, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return nil, fmt.Errorf("read CA: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(ca) {
			return nil, fmt.Errorf("parse CA cert")
		}
		tlsCfg.RootCAs = pool
	}
	return tlsCfg, nil
}

// configProxy satisfies runner.ConfigSource. It delegates to the link client
// so the runner can read the Config the core sends after connecting.
type configProxy struct {
	client *workerlink.Client
}

func (p *configProxy) CoreConfig() *linkv1.Config {
	if p.client == nil {
		return nil
	}
	return p.client.CoreConfig()
}
