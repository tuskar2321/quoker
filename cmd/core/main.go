// Command core runs the Quoker core: public API, worker link, storage.
// See docs/02-architecture.md.
//
// Usage:
//
//	core [serve]                        run the server
//	core admin bootstrap                print the first admin invite
//	core settings set <key> <value|->   store a setting; "-" reads stdin
//	core release add <version_code> <version_name> <apk> [changelog|-]
//	                                    publish an APK; "-" reads the changelog from stdin
//	core release list                   list published APKs
//	core workers allow <id> <fingerprint>
//	                                    allow a worker certificate (SHA-256 hex)
//	core backup                         VACUUM INTO $BACKUP_DIR/quoker-<UTC time>.db, keep 7, print the path
//	core healthcheck                    exit 0 if GET 127.0.0.1:<HTTP_ADDR port>/healthz answers 200
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
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	_ "time/tzdata" // Europe/Moscow even where the image has no zoneinfo

	"google.golang.org/grpc"

	"github.com/tuskar2321/quoker/internal/core/accounts"
	"github.com/tuskar2321/quoker/internal/core/admin"
	"github.com/tuskar2321/quoker/internal/core/backup"
	"github.com/tuskar2321/quoker/internal/core/bot"
	"github.com/tuskar2321/quoker/internal/core/botapi"
	"github.com/tuskar2321/quoker/internal/core/config"
	"github.com/tuskar2321/quoker/internal/core/files"
	"github.com/tuskar2321/quoker/internal/core/history"
	"github.com/tuskar2321/quoker/internal/core/httpapi"
	corelink "github.com/tuskar2321/quoker/internal/core/link"
	"github.com/tuskar2321/quoker/internal/core/metrics"
	"github.com/tuskar2321/quoker/internal/core/proxy"
	"github.com/tuskar2321/quoker/internal/core/queue"
	"github.com/tuskar2321/quoker/internal/core/ratelimit"
	"github.com/tuskar2321/quoker/internal/core/releases"
	"github.com/tuskar2321/quoker/internal/core/results"
	"github.com/tuskar2321/quoker/internal/core/sends"
	"github.com/tuskar2321/quoker/internal/core/settings"
	"github.com/tuskar2321/quoker/internal/core/store"
	"github.com/tuskar2321/quoker/internal/core/targets"
	"github.com/tuskar2321/quoker/internal/core/videos"
	"github.com/tuskar2321/quoker/internal/shared/logging"

	linkv1 "github.com/tuskar2321/quoker/gen/quoker/link/v1"
)

// version is set at build time: -ldflags "-X main.version=<tag>".
var version = "dev"

const (
	// shutdownTimeout is the graceful shutdown budget (docs/11-nfr-security.md).
	shutdownTimeout = 20 * time.Second
	cliTimeout      = 30 * time.Second
	usage           = "usage: core [serve] | core admin bootstrap | core settings set <key> <value|-> | " +
		"core release add <version_code> <version_name> <apk> [changelog|-] | core release list | " +
		"core workers allow <id> <fingerprint> | core backup | core healthcheck"
)

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "core:", err)
		os.Exit(1)
	}
}

func run(args []string, stdin io.Reader, stdout io.Writer) error {
	if len(args) == 1 && args[0] == "healthcheck" {
		// Needs only HTTP_ADDR: it must work with a minimal environment.
		return healthcheck(or(os.Getenv("HTTP_ADDR"), ":8080"))
	}
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
	case len(args) >= 5 && len(args) <= 6 && args[0] == "release" && args[1] == "add":
		code, err := strconv.ParseInt(args[2], 10, 64)
		if err != nil {
			return fmt.Errorf("version_code %q: %w", args[2], err)
		}
		changelog := ""
		if len(args) == 6 {
			if changelog = args[5]; changelog == "-" {
				if changelog, err = readAll(stdin); err != nil {
					return err
				}
			}
		}
		return withStore(cfg, func(ctx context.Context, st *store.Store) error {
			r, err := releases.New(st, cfg.ReleasesDir, nil).Add(ctx, code, args[3], args[4], changelog)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(stdout, "published %s (%d) as %s, sha256 %s\n", r.VersionName, r.VersionCode, r.FileName, r.SHA256)
			return err
		})
	case len(args) == 4 && args[0] == "workers" && args[1] == "allow":
		// The first worker is allowed here: the bot needs a worker to talk to Telegram.
		return withStore(cfg, func(ctx context.Context, st *store.Store) error {
			if err := admin.AllowWorker(ctx, st.DB(), time.Now(), args[2], args[3]); err != nil {
				return err
			}
			_, err := fmt.Fprintf(stdout, "worker %s allowed\n", args[2])
			return err
		})
	case len(args) == 1 && args[0] == "backup":
		if cfg.BackupDir == "" {
			return errors.New("BACKUP_DIR is required")
		}
		return withStore(cfg, func(ctx context.Context, st *store.Store) error {
			path, err := backup.New(st.DB(), cfg.BackupDir, nil).Run(ctx)
			if path != "" {
				_, _ = fmt.Fprintln(stdout, path)
			}
			return err
		})
	case len(args) == 2 && args[0] == "release" && args[1] == "list":
		return withStore(cfg, func(ctx context.Context, st *store.Store) error {
			rows, err := st.DB().QueryContext(ctx,
				`SELECT version_code, version_name, apk_path, published_at FROM app_releases ORDER BY version_code DESC`)
			if err != nil {
				return err
			}
			defer func() { _ = rows.Close() }()
			for rows.Next() {
				var (
					code, ms   int64
					name, file string
				)
				if err := rows.Scan(&code, &name, &file, &ms); err != nil {
					return err
				}
				if _, err := fmt.Fprintf(stdout, "%d\t%s\t%s\t%s\n", code, name, file, time.UnixMilli(ms).UTC().Format(time.RFC3339)); err != nil {
					return err
				}
			}
			return rows.Err()
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
	base, err := logging.New(os.Stdout, cfg.LogLevel, "")
	if err != nil {
		return err
	}
	log := base.With("component", "core")
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

	trusted, err := proxy.Parse(cfg.TrustedProxies)
	if err != nil {
		return err
	}
	set := settings.New(st.DB(), nil)
	q := queue.New(st, nil)
	videosSvc := videos.New(st, q, nil)
	botSvc := bot.New(st, set, nil)
	caller := botapi.New(q)
	accSvc := accounts.New(st, nil, nil)
	targetsSvc := targets.New(st, nil).WithBot(caller)
	sendsSvc := sends.New(st, q, videosSvc, targetsSvc, nil)
	cache, err := files.New(st, cfg.CacheDir, cfg.CacheMaxBytes, nil, base.With("component", "files"))
	if err != nil {
		return err
	}
	// Background work is tracked so the store closes only after it stops.
	var bg sync.WaitGroup
	spawn := func(fn func(context.Context)) {
		bg.Add(1)
		go func() {
			defer bg.Done()
			fn(ctx)
		}()
	}
	// Before serving: no video may claim a file the disk lost (docs/11).
	if _, err := cache.Reconcile(ctx); err != nil {
		return fmt.Errorf("reconcile cache: %w", err)
	}
	spawn(func(ctx context.Context) { cache.Run(ctx, files.CleanupInterval) })
	releasesSvc := releases.New(st, cfg.ReleasesDir, nil)
	historySvc := history.New(st, nil)
	spawn(func(ctx context.Context) {
		historySvc.Run(ctx, history.CleanupInterval, func(err error) { log.Warn("history cleanup", "err", err) })
	})
	spawn(func(ctx context.Context) {
		ratelimit.RunPurge(ctx, st, time.Hour, 24*time.Hour, func(err error) { log.Warn("rate limit purge", "err", err) })
	})
	if cfg.UploadBase == "" {
		log.Warn("UPLOAD_BASE and DOMAIN are empty: workers cannot upload covers and files")
	}
	if cfg.PublicURL == "" {
		log.Warn("PUBLIC_URL and DOMAIN are empty: apk_url will follow the request's Host header")
	}

	// The bot handler needs admin, which needs the link server; updates only
	// arrive once gRPC serves, by which time botHandler is set.
	var botHandler *bot.Handler
	reg := metrics.New()
	linkSrv := corelink.New(ctx, corelink.Deps{
		Store:    st,
		Queue:    q,
		Settings: set,
		Results:  results.New(videosSvc, sendsSvc, cache),
		Updates:  corelink.UpdateFunc(func(ctx context.Context, u []byte) error { return botHandler.HandleUpdate(ctx, u) }),
		// A new token may be a new bot: learn its username, register commands.
		OnBotTokenChange: func() { spawn(botHandler.Start) },
		UploadBase:       cfg.UploadBase,
		Trusted:          &trusted,
		Metrics:          reg,
		Log:              base.With("component", "link"),
	})
	registerGauges(reg, st, linkSrv, cache)
	adminSvc := admin.New(admin.Deps{
		Store: st, Settings: set, Link: linkSrv, Files: cache, Bot: caller,
		Log: base.With("component", "admin"),
	})
	botHandler = bot.NewHandler(bot.HandlerDeps{
		Link: botSvc, Accounts: accSvc, Targets: targetsSvc, Settings: set, Bot: caller, Admin: adminSvc,
		Log: base.With("component", "bot"),
	})
	spawn(botHandler.Start)
	spawn(adminSvc.Run)
	if cfg.BackupDir != "" {
		loc, err := time.LoadLocation("Europe/Moscow")
		if err != nil {
			return fmt.Errorf("load backup timezone: %w", err)
		}
		sched := &backup.Scheduler{Backup: backup.New(st.DB(), cfg.BackupDir, nil), Loc: loc, Log: base.With("component", "backup")}
		spawn(sched.Run)
		log.Info("daily backup scheduled", "dir", cfg.BackupDir, "at", "04:00 Europe/Moscow")
	}
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
			Accounts:  accSvc,
			Bot:       botSvc,
			Videos:    videosSvc,
			Targets:   targetsSvc,
			Sends:     sendsSvc,
			Files:     cache,
			History:   historySvc,
			Releases:  releasesSvc,
			PublicURL: cfg.PublicURL,
			Spawn:     spawn,
			Metrics:   reg,
			Trusted:   trusted,
			Admin:     adminSvc,
			Store:     st,
			Version:   version,
			Settings:  set,
			Ready:     st.Ready,
			Log:       base.With("component", "api"),
		}),
		ReadHeaderTimeout: 10 * time.Second,
		// Bounds reading the request; responses (file downloads) are not
		// affected. Uploads go to the files server, which has no such cap.
		ReadTimeout: 30 * time.Second,
		IdleTimeout: 2 * time.Minute,
		ErrorLog:    slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}
	httpErrc := make(chan error, 1)
	go func() {
		log.Info("http listening", "addr", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			httpErrc <- fmt.Errorf("http server: %w", err)
		}
		close(httpErrc)
	}()

	// Worker uploads; reachable only through Caddy's mTLS listener.
	filesSrv := &http.Server{
		Addr:              cfg.FilesAddr,
		Handler:           internalMux(cache.InternalHandler(linkSrv.VerifyWorker, trusted, base.With("component", "files")), reg, trusted, base),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}
	filesErrc := make(chan error, 1)
	go func() {
		log.Info("files listening", "addr", cfg.FilesAddr)
		if err := filesSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			filesErrc <- fmt.Errorf("files server: %w", err)
		}
		close(filesErrc)
	}()

	select {
	case err := <-errc:
		return err
	case err := <-httpErrc:
		return err
	case err := <-filesErrc:
		return err
	case <-ctx.Done():
	}
	log.Info("shutting down")
	shCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	// ctx is cancelled, so the link server is already sending Shutdown and
	// closing worker streams; HTTP servers drain in parallel with gRPC.
	grpcDone := make(chan struct{})
	go func() {
		gs.GracefulStop()
		close(grpcDone)
	}()
	var wg sync.WaitGroup
	var httpErr error
	wg.Add(2)
	go func() {
		defer wg.Done()
		if err := filesSrv.Shutdown(shCtx); err != nil {
			log.Warn("files shutdown", "err", err)
		}
	}()
	go func() {
		defer wg.Done()
		if err := srv.Shutdown(shCtx); err != nil {
			httpErr = fmt.Errorf("http shutdown: %w", err)
		}
	}()
	select {
	case <-grpcDone:
	case <-shCtx.Done():
		log.Warn("grpc did not stop in time, closing connections")
		gs.Stop()
	}
	wg.Wait()
	// ctx is cancelled: background loops are stopping; let them finish
	// before the deferred store close.
	done := make(chan struct{})
	go func() {
		bg.Wait()
		linkSrv.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		log.Warn("background work did not stop in time")
	}
	if httpErr != nil {
		return httpErr
	}
	return <-httpErrc
}

// internalMux serves worker uploads and /metrics on the internal port. Caddy
// forwards only /internal/* there, so /metrics is reachable from the docker
// network (a Prometheus next to the core), never from outside.
func internalMux(uploads http.Handler, reg *metrics.Registry, trusted proxy.Trusted, base *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/metrics", proxy.Only(trusted, reg.Handler(base.With("component", "metrics"))))
	mux.Handle("/", uploads)
	return mux
}

// registerGauges adds the values read at scrape time (docs/11).
func registerGauges(reg *metrics.Registry, st *store.Store, link *corelink.Server, cache *files.Store) {
	reg.GaugeFunc("quoker_queue_tasks", "Tasks waiting or running, by kind and status.",
		func(ctx context.Context) ([]metrics.Sample, error) {
			rows, err := st.DB().QueryContext(ctx,
				`SELECT kind, status, COUNT(*) FROM tasks WHERE status IN ('queued', 'leased') GROUP BY kind, status`)
			if err != nil {
				return nil, err
			}
			defer func() { _ = rows.Close() }()
			var out []metrics.Sample
			for rows.Next() {
				var kind, status string
				var n float64
				if err := rows.Scan(&kind, &status, &n); err != nil {
					return nil, err
				}
				out = append(out, metrics.Sample{Values: []string{kind, status}, Value: n})
			}
			return out, rows.Err()
		}, "kind", "status")
	reg.GaugeFunc("quoker_workers_connected", "Workers with a live stream.",
		func(context.Context) ([]metrics.Sample, error) {
			return []metrics.Sample{{Value: float64(len(link.ConnectedWorkers()))}}, nil
		})
	reg.GaugeFunc("quoker_cache_bytes", "Bytes held by the file cache.",
		func(context.Context) ([]metrics.Sample, error) {
			n, err := cache.UsedBytes()
			return []metrics.Sample{{Value: float64(n)}}, err
		})
	reg.GaugeFunc("quoker_cache_limit_bytes", "File cache limit.",
		func(context.Context) ([]metrics.Sample, error) {
			return []metrics.Sample{{Value: float64(cache.MaxBytes())}}, nil
		})
}

func or(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
