// Command faketelegram runs an in-memory Telegram Bot API for local development
// and end-to-end checks (`make dev`). See internal/devtools/faketelegram.
//
// Usage:
//
//	faketelegram [serve] [flags]   serve the Bot API over HTTPS and the admin API over HTTP
//	faketelegram healthcheck       exit 0 when the admin API answers /healthz
//
// The Bot API host is served with a certificate from a throwaway CA. The CA
// bundle (system roots plus the CA) is written to -bundle, so clients that read
// SSL_CERT_FILE trust the fake for api.telegram.org.
package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/tuskar2321/quoker/internal/devtools/faketelegram"
	"github.com/tuskar2321/quoker/internal/shared/logging"
)

const (
	defaultAdminAddr  = "127.0.0.1:8099"
	shutdownTimeout   = 5 * time.Second
	healthcheckBudget = 3 * time.Second
)

type options struct {
	token          string
	botID          int64
	botUsername    string
	tlsAddr        string
	adminAddr      string
	tlsHost        string
	caDir          string
	bundle         string
	systemRoots    string
	maxUploadBytes int64
	logLevel       string
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "faketelegram:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) > 0 && args[0] == "healthcheck" {
		return healthcheck(args[1:])
	}
	if len(args) > 0 && args[0] == "serve" {
		args = args[1:]
	}
	opts, err := parseServe(args)
	if err != nil {
		return err
	}
	return serve(opts)
}

func parseServe(args []string) (options, error) {
	var o options
	fs := flag.NewFlagSet("faketelegram", flag.ContinueOnError)
	fs.StringVar(&o.token, "token", os.Getenv("FAKE_TELEGRAM_TOKEN"), "bot token clients must use (default $FAKE_TELEGRAM_TOKEN)")
	fs.Int64Var(&o.botID, "bot-id", 1000001, "bot user id")
	fs.StringVar(&o.botUsername, "bot-username", "quoker_dev_bot", "bot username")
	fs.StringVar(&o.tlsAddr, "tls-addr", ":443", "Bot API listen address (HTTPS)")
	fs.StringVar(&o.adminAddr, "admin-addr", ":8099", "admin API listen address (plain HTTP, keep it private)")
	fs.StringVar(&o.tlsHost, "tls-host", "api.telegram.org", "host name on the Bot API certificate")
	fs.StringVar(&o.caDir, "ca-dir", "/ca", "directory for the CA key and certificate (persisted between runs)")
	fs.StringVar(&o.bundle, "bundle", "/ca/ca-bundle.pem", "CA bundle to write for SSL_CERT_FILE")
	fs.StringVar(&o.systemRoots, "system-roots", "/etc/ssl/certs/ca-certificates.crt", "system CA bundle to prepend to -bundle, if present")
	fs.Int64Var(&o.maxUploadBytes, "max-upload-bytes", 50<<20, "largest sendVideo accepted (Telegram cloud limit)")
	fs.StringVar(&o.logLevel, "log-level", "info", "debug, info, warn or error")
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	if o.token == "" {
		return options{}, errors.New("a bot token is required: -token or FAKE_TELEGRAM_TOKEN")
	}
	if fs.NArg() > 0 {
		return options{}, fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	return o, nil
}

func serve(o options) error {
	log, err := logging.New(os.Stdout, o.logLevel, "faketelegram")
	if err != nil {
		return err
	}
	now := time.Now()
	authority, err := faketelegram.LoadAuthority(o.caDir, now)
	if err != nil {
		return fmt.Errorf("CA: %w", err)
	}
	if err := authority.WriteBundle(o.bundle, o.systemRoots); err != nil {
		return fmt.Errorf("CA bundle: %w", err)
	}
	tlsCfg, err := authority.ServerTLSConfig(o.tlsHost, now)
	if err != nil {
		return fmt.Errorf("server certificate: %w", err)
	}
	lis, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", o.tlsAddr)
	if err != nil {
		return fmt.Errorf("bot api listen: %w", err)
	}
	tlsLis := tls.NewListener(lis, tlsCfg)

	st := faketelegram.NewState(faketelegram.Config{
		Token:          o.token,
		BotID:          o.botID,
		BotUsername:    o.botUsername,
		MaxUploadBytes: o.maxUploadBytes,
	})
	srv := faketelegram.NewServer(st, log)
	botHTTP := &http.Server{
		Handler:           srv.BotHandler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       5 * time.Minute, // uploads of up to the limit
		WriteTimeout:      2 * time.Minute, // long polls are 50 seconds
		IdleTimeout:       2 * time.Minute,
	}
	adminHTTP := &http.Server{
		Addr:              o.adminAddr,
		Handler:           srv.AdminHandler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errc := make(chan error, 2)
	go func() { errc <- botHTTP.Serve(tlsLis) }()
	go func() { errc <- adminHTTP.ListenAndServe() }()
	log.Info("fake telegram listening", "bot_api", o.tlsAddr, "admin", o.adminAddr, "tls_host", o.tlsHost, "bundle", o.bundle)

	var runErr error
	select {
	case <-ctx.Done():
	case runErr = <-errc:
	}
	shCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	_ = botHTTP.Shutdown(shCtx)
	_ = adminHTTP.Shutdown(shCtx)
	if runErr != nil && !errors.Is(runErr, http.ErrServerClosed) {
		return runErr
	}
	return nil
}

// healthcheck probes the admin API from inside the container. Compose runs it.
func healthcheck(args []string) error {
	fs := flag.NewFlagSet("healthcheck", flag.ContinueOnError)
	addr := fs.String("admin-addr", defaultAdminAddr, "admin API address to probe")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), healthcheckBudget)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+*addr+"/healthz", nil)
	if err != nil {
		return err
	}
	res, err := (&http.Client{Timeout: healthcheckBudget}).Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<10))
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("healthz: %s", res.Status)
	}
	return nil
}
