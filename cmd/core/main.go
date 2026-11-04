// Command core runs the Quoker core: public API, worker link, storage.
// See docs/02-architecture.md.
//
// Usage:
//
//	core [serve]                        run the server
//	core admin bootstrap                print the first admin invite
//	core settings set <key> <value|->   store a setting; "-" reads stdin
package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"google.golang.org/grpc"

	"github.com/tuskar2321/quoker/internal/core/accounts"
	"github.com/tuskar2321/quoker/internal/core/bot"
	"github.com/tuskar2321/quoker/internal/core/config"
	"github.com/tuskar2321/quoker/internal/core/httpapi"
	corelink "github.com/tuskar2321/quoker/internal/core/link"
	"github.com/tuskar2321/quoker/internal/core/queue"
	"github.com/tuskar2321/quoker/internal/core/results"
	"github.com/tuskar2321/quoker/internal/core/sends"
	"github.com/tuskar2321/quoker/internal/core/settings"
	"github.com/tuskar2321/quoker/internal/core/store"
	"github.com/tuskar2321/quoker/internal/core/targets"
	"github.com/tuskar2321/quoker/internal/core/videos"
	"github.com/tuskar2321/quoker/internal/shared/logging"

	linkv1 "github.com/tuskar2321/quoker/gen/quoker/link/v1"
)

const (
	// shutdownTimeout is the graceful shutdown budget (docs/11-nfr-security.md).
	shutdownTimeout = 20 * time.Second
	cliTimeout      = 30 * time.Second
	usage           = "usage: core [serve] | core admin bootstrap | core settings set <key> <value|->"
)

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "core:", err)
		os.Exit(1)
	}
}

func run(args []string, stdin io.Reader, stdout io.Writer) error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}
	switch {
	case len(args) == 0 || (len(args) == 1 && args[0] == "serve"):
		return serve(cfg)
	case len(args) == 2 && args[0] == "admin" && args[1] == "bootstrap":
		return withStore(cfg, func(ctx context.Context, st *store.Store) error {
			code, expires, err := accounts.New(st, nil, nil).CreateBootstrapInvite(ctx)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(stdout, "admin invite: %s (expires %s)\n", code, expires.Format(time.RFC3339))
			return err
		})
	case len(args) == 4 && args[0] == "settings" && args[1] == "set":
		key, err := settings.ParseKey(args[2])
		if err != nil {
			return err
		}
		value := args[3]
		if value == "-" { // keeps secrets out of shell history and ps
			if value, err = readAll(stdin); err != nil {
				return err
			}
		}
		return withStore(cfg, func(ctx context.Context, st *store.Store) error {
			if err := settings.New(st.DB(), nil).Set(ctx, key, value); err != nil {
				return err
			}
			_, err := fmt.Fprintf(stdout, "%s updated\n", key)
			return err
		})
	}
	return errors.New(usage)
}

func readAll(r io.Reader) (string, error) {
	b, err := io.ReadAll(bufio.NewReader(r))
	if err != nil {
		return "", fmt.Errorf("read stdin: %w", err)
	}
	return strings.TrimRight(string(b), "\r\n"), nil
}

func withStore(cfg config.Config, fn func(context.Context, *store.Store) error) error {
	ctx, cancel := context.WithTimeout(context.Background(), cliTimeout)
	defer cancel()
	st, err := store.Open(ctx, cfg.DBPath)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()
	return fn(ctx, st)
}

func serve(cfg config.Config) error {
	log, err := logging.New(os.Stdout, cfg.LogLevel, "core")
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	openCtx, cancel := context.WithTimeout(ctx, cliTimeout)
	st, err := store.Open(openCtx, cfg.DBPath)
	cancel()
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()
	log.Info("database ready", "path", cfg.DBPath)

	set := settings.New(st.DB(), nil)
	q := queue.New(st, nil)
	videosSvc := videos.New(st, q, nil)
	botSvc := bot.New(st, set, nil)
	targetsSvc := targets.New(st, nil)
	sendsSvc := sends.New(st, q, videosSvc, targetsSvc, nil)

	linkSrv := corelink.New(ctx, corelink.Deps{
		Store:    st,
		Queue:    q,
		Settings: set,
		Results:  results.New(videosSvc, sendsSvc),
		Log:      log.With("component", "link"),
	})
	gs := grpc.NewServer()
	linkv1.RegisterLinkServiceServer(gs, linkSrv)

	grpcLis, err := (&net.ListenConfig{}).Listen(ctx, "tcp", cfg.GRPCAddr)
	if err != nil {
		return fmt.Errorf("grpc listen: %w", err)
	}

	errc := make(chan error, 2)
	go func() {
		log.Info("grpc listening", "addr", cfg.GRPCAddr)
		if err := gs.Serve(grpcLis); err != nil {
			errc <- fmt.Errorf("grpc server: %w", err)
		}
		close(errc)
	}()

	srv := &http.Server{
		Addr: cfg.HTTPAddr,
		Handler: httpapi.New(httpapi.Deps{
			Accounts: accounts.New(st, nil, nil),
			Bot:      botSvc,
			Videos:   videosSvc,
			Targets:  targetsSvc,
			Sends:    sendsSvc,
			Settings: set,
			Ready:    st.Ready,
			Log:      log,
		}),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}
	httpErrc := make(chan error, 1)
	go func() {
		log.Info("http listening", "addr", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			httpErrc <- fmt.Errorf("http server: %w", err)
		}
		close(httpErrc)
	}()

	select {
	case err := <-errc:
		return err
	case err := <-httpErrc:
		return err
	case <-ctx.Done():
	}
	log.Info("shutting down")
	shCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	gs.GracefulStop()
	if err := srv.Shutdown(shCtx); err != nil {
		return fmt.Errorf("http shutdown: %w", err)
	}
	return <-httpErrc
}
