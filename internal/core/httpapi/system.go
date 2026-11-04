package httpapi

import (
	"context"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/tuskar2321/quoker/internal/core/admin"
	"github.com/tuskar2321/quoker/internal/shared/errs"
)

// refreshTimeout bounds the background rights re-check.
const refreshTimeout = 2 * time.Minute

// refreshTargets handles POST /v1/targets/refresh: 202 at once, the core
// re-checks the user's rights in the background through the bot.
func (s *server) refreshTargets(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	u, err := s.acc.User(r.Context(), p.UserID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if !u.TelegramLinked() {
		writeError(w, r, errs.New(errs.TelegramNotLinked, "telegram is not linked"))
		return
	}
	if !s.refreshing.start(u.ID) {
		w.WriteHeader(http.StatusAccepted) // the re-check already running covers this request
		return
	}
	if overLimit(w, r, s.refreshLimit, u.ID, "") {
		s.refreshing.done(u.ID)
		return
	}
	log := loggerFrom(r.Context())
	// Outlives the request on purpose: the client gets 202 and polls targets.
	bg := context.WithoutCancel(r.Context())
	go func() {
		defer s.refreshing.done(u.ID)
		ctx, cancel := context.WithTimeout(bg, refreshTimeout)
		defer cancel()
		if err := s.targets.Refresh(ctx, u.ID, u.TelegramUserID); err != nil {
			log.Warn("targets refresh", "user_id", u.ID, "err", err)
		}
	}()
	w.WriteHeader(http.StatusAccepted)
}

type workerDTO struct {
	ID           string  `json:"id"`
	Fingerprint  string  `json:"fingerprint"`
	Status       string  `json:"status"`
	Connected    bool    `json:"connected"`
	Version      *string `json:"version"`
	YtdlpVersion *string `json:"ytdlp_version"`
	LastSeenAt   *string `json:"last_seen_at"`
	IsPoller     bool    `json:"is_poller"`
}

func toWorkerDTOs(ws []admin.Worker) []workerDTO {
	out := make([]workerDTO, len(ws))
	for i, w := range ws {
		out[i] = workerDTO{
			ID: w.ID, Fingerprint: w.Fingerprint, Status: w.Status, Connected: w.Connected,
			Version: strDTO(w.Version), YtdlpVersion: strDTO(w.YtdlpVersion),
			LastSeenAt: timeDTO(w.LastSeenAt), IsPoller: w.IsPoller,
		}
	}
	return out
}

// adminStatus handles GET /v1/admin/status.
func (s *server) adminStatus(w http.ResponseWriter, r *http.Request) {
	st, err := s.admin.Status(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	var appLatest string
	_ = s.st.DB().QueryRowContext(r.Context(),
		`SELECT version_name FROM app_releases ORDER BY version_code DESC LIMIT 1`).Scan(&appLatest)
	type queueDTO struct {
		Queued  int `json:"queued"`
		Running int `json:"running"`
	}
	type diskDTO struct {
		UsedBytes  int64 `json:"used_bytes"`
		TotalBytes int64 `json:"total_bytes"`
		CacheBytes int64 `json:"cache_bytes"`
	}
	writeJSON(w, http.StatusOK, struct {
		Workers   []workerDTO       `json:"workers"`
		Queue     queueDTO          `json:"queue"`
		Errors24h map[string]int    `json:"errors_24h"`
		Disk      diskDTO           `json:"disk"`
		Versions  map[string]string `json:"versions"`
	}{
		Workers:   toWorkerDTOs(st.Workers),
		Queue:     queueDTO{st.Queued, st.Running},
		Errors24h: st.Errors24h,
		Disk:      diskDTO{st.Disk.UsedBytes, st.Disk.TotalBytes, st.Disk.CacheBytes},
		Versions:  map[string]string{"core": s.version, "app_latest": appLatest},
	})
}

// listWorkers handles GET /v1/admin/workers.
func (s *server) listWorkers(w http.ResponseWriter, r *http.Request) {
	ws, err := s.admin.Workers(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Items []workerDTO `json:"items"`
	}{toWorkerDTOs(ws)})
}

// allowWorker handles POST /v1/admin/workers.
func (s *server) allowWorker(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID          string `json:"id"`
		Fingerprint string `json:"fingerprint"`
	}
	if err := decodeJSON(r, &req, false); err != nil {
		writeError(w, r, err)
		return
	}
	if err := s.admin.AllowWorker(r.Context(), req.ID, req.Fingerprint); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusCreated)
}

// revokeWorker handles DELETE /v1/admin/workers/{worker_id}.
func (s *server) revokeWorker(w http.ResponseWriter, r *http.Request) {
	if err := s.admin.RevokeWorker(r.Context(), chi.URLParam(r, "worker_id")); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// putInstagramCookies handles PUT /v1/admin/instagram-cookies.
func (s *server) putInstagramCookies(w http.ResponseWriter, r *http.Request) {
	b, err := io.ReadAll(io.LimitReader(r.Body, admin.MaxCookiesBytes+1))
	if err != nil {
		writeError(w, r, errs.Wrap(errs.InvalidRequest, err))
		return
	}
	if err := s.admin.SetCookies(r.Context(), string(b)); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// inFlight tracks users with a target re-check running.
type inFlight struct {
	mu      sync.Mutex
	running map[int64]bool
}

func (f *inFlight) start(userID int64) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.running[userID] {
		return false
	}
	f.running[userID] = true
	return true
}

func (f *inFlight) done(userID int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.running, userID)
}
