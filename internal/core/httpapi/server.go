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
	"github.com/tuskar2321/quoker/internal/core/ratelimit"
	"github.com/tuskar2321/quoker/internal/core/settings"
	"github.com/tuskar2321/quoker/internal/shared/errs"
)

const (
	// registerPerHour is the registration limit per IP.
	registerPerHour = 5
	// apiTimeout bounds JSON endpoints; file streaming gets its own limits.
	apiTimeout = 15 * time.Second
)

// Deps are the dependencies of the API.
type Deps struct {
	Accounts *accounts.Service
	Settings *settings.Store
	Ready    func(context.Context) error // /readyz probe
	Log      *slog.Logger
	Now      func() time.Time // defaults to time.Now
}

type server struct {
	acc      *accounts.Service
	settings *settings.Store
	ready    func(context.Context) error
	now      func() time.Time
	register *ratelimit.Memory
}

// New builds the HTTP handler of the public API.
func New(d Deps) http.Handler {
	if d.Now == nil {
		d.Now = time.Now
	}
	s := &server{
		acc:      d.Accounts,
		settings: d.Settings,
		ready:    d.Ready,
		now:      d.Now,
		register: ratelimit.NewMemory(registerPerHour, time.Hour, d.Now),
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
		r.Use(limitBody, middleware.Timeout(apiTimeout), requireVersion(d.Settings))

		r.Post("/auth/register", s.registerUser)

		r.Group(func(r chi.Router) {
			r.Use(requireSession(d.Accounts))
			r.Post("/auth/logout", s.logout)
			r.Get("/me", s.me)

			r.Route("/admin", func(r chi.Router) {
				r.Use(requireAdmin)
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
