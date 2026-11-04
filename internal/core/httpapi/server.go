// Package httpapi serves the public REST API of the core for the Android app.
// The contract is docs/03-public-api.md and api/openapi.yaml.
package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/tuskar2321/quoker/internal/core/accounts"
	"github.com/tuskar2321/quoker/internal/core/admin"
	"github.com/tuskar2321/quoker/internal/core/bot"
	"github.com/tuskar2321/quoker/internal/core/files"
	"github.com/tuskar2321/quoker/internal/core/ratelimit"
	"github.com/tuskar2321/quoker/internal/core/sends"
	"github.com/tuskar2321/quoker/internal/core/settings"
	"github.com/tuskar2321/quoker/internal/core/store"
	"github.com/tuskar2321/quoker/internal/core/targets"
	"github.com/tuskar2321/quoker/internal/core/videos"
	"github.com/tuskar2321/quoker/internal/shared/errs"
)

const (
	// registerPerHour is the registration limit per IP.
	registerPerHour = 5
	// sendsPerHour is the send limit per user.
	sendsPerHour = 30
	// streamsPerSession caps parallel file downloads (docs/03-public-api.md).
	streamsPerSession = 2
	// apiTimeout bounds JSON endpoints; file streaming gets its own limits.
	apiTimeout = 15 * time.Second
)

// Deps are the dependencies of the API.
type Deps struct {
	Accounts *accounts.Service
	Bot      *bot.Service
	Videos   *videos.Service
	Targets  *targets.Service
	Sends    *sends.Service
	Files    *files.Store
	Admin    *admin.Service
	Store    *store.Store // read-only queries for admin status
	Settings *settings.Store
	Version  string                      // core build version
	Ready    func(context.Context) error // /readyz probe
	Log      *slog.Logger
	Now      func() time.Time // defaults to time.Now
}

type server struct {
	acc       *accounts.Service
	bot       *bot.Service
	videos    *videos.Service
	targets   *targets.Service
	sends     *sends.Service
	files     *files.Store
	admin     *admin.Service
	st        *store.Store
	version   string
	settings  *settings.Store
	ready     func(context.Context) error
	now       func() time.Time
	register  *ratelimit.Memory
	sendLimit *ratelimit.Memory
	streams   *streamLimiter
}

// New builds the HTTP handler of the public API.
func New(d Deps) http.Handler {
	if d.Now == nil {
		d.Now = time.Now
	}
	s := &server{
		acc:       d.Accounts,
		bot:       d.Bot,
		videos:    d.Videos,
		targets:   d.Targets,
		sends:     d.Sends,
		files:     d.Files,
		admin:     d.Admin,
		st:        d.Store,
		version:   d.Version,
		settings:  d.Settings,
		ready:     d.Ready,
		now:       d.Now,
		register:  ratelimit.NewMemory(registerPerHour, time.Hour, d.Now),
		sendLimit: ratelimit.NewMemory(sendsPerHour, time.Hour, d.Now),
		streams:   newStreamLimiter(streamsPerSession),
	}

	r := chi.NewRouter()
	r.Use(accessLog(d.Log), recoverer)
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, r, errs.New(errs.NotFound, "no such route"))
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, r, errs.New(errs.NotFound, "method not allowed"))
	})

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	r.Get("/readyz", s.readyz)

	r.Route("/v1", func(r chi.Router) {
		r.Use(limitBody, requireVersion(d.Settings))

		// File streaming is bounded by the client, not by apiTimeout.
		r.With(requireSession(d.Accounts)).Get("/videos/{video_id}/file", s.videoFile)

		r.Group(func(r chi.Router) {
			r.Use(middleware.Timeout(apiTimeout))
			r.Post("/auth/register", s.registerUser)
		})

		r.Group(func(r chi.Router) {
			r.Use(middleware.Timeout(apiTimeout), requireSession(d.Accounts))
			r.Post("/auth/logout", s.logout)
			r.Get("/me", s.me)
			r.Post("/auth/telegram/link", s.createTelegramLink)
			r.Get("/auth/telegram/status", s.telegramLinkStatus)
			r.Delete("/auth/telegram", s.unlinkTelegram)

			r.Post("/videos/resolve", s.resolveVideo)
			r.Get("/videos/{video_id}", s.getVideo)
			r.Get("/videos/{video_id}/thumbnail", s.videoThumbnail)
			r.Post("/videos/{video_id}/prepare", s.prepareVideo)
			r.Post("/videos/{video_id}/downloaded", s.videoDownloaded)
			r.Get("/tasks/{task_id}", s.getTask)

			r.Get("/targets", s.listTargets)
			r.Post("/targets/refresh", s.refreshTargets)
			r.Patch("/targets/{id}", s.patchTarget)

			r.Post("/sends", s.createSend)
			r.Get("/sends/{id}", s.getSend)

			r.Route("/admin", func(r chi.Router) {
				r.Use(requireAdmin)
				r.Get("/status", s.adminStatus)
				r.Get("/workers", s.listWorkers)
				r.Post("/workers", s.allowWorker)
				r.Delete("/workers/{worker_id}", s.revokeWorker)
				r.Put("/instagram-cookies", s.putInstagramCookies)
				r.Get("/invites", s.listInvites)
				r.Post("/invites", s.createInvite)
				r.Delete("/invites/{invite_id}", s.revokeInvite)
				r.Get("/users", s.listUsers)
				r.Post("/users/{user_id}/block", s.blockUser)
				r.Post("/users/{user_id}/unblock", s.unblockUser)
			})
		})
	})
	return r
}

func (s *server) readyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.ready(ctx); err != nil {
		loggerFrom(ctx).WarnContext(ctx, "not ready", "error", err.Error())
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
}
