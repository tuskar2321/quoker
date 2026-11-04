package files

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/tuskar2321/quoker/internal/shared/errs"
)

// HeaderFingerprint is set by Caddy after verifying the worker's certificate.
const HeaderFingerprint = "X-Client-Cert-Fingerprint"

// VerifyWorker resolves a certificate fingerprint to an allowed worker id.
type VerifyWorker func(ctx context.Context, fingerprint string) (workerID string, err error)

// InternalHandler serves the worker upload endpoints on the internal port
// (docs/04-core-worker-protocol.md). It must never be exposed directly: it
// trusts the fingerprint header, which only Caddy may set.
func (s *Store) InternalHandler(verify VerifyWorker, log *slog.Logger) http.Handler {
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fp := r.Header.Get(HeaderFingerprint)
			workerID, err := verify(r.Context(), fp)
			if fp == "" || err != nil {
				log.Warn("upload rejected", "path", r.URL.Path, "err", err)
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), workerKey{}, workerID)))
		})
	})
	r.Route("/internal/v1", func(r chi.Router) {
		r.Head("/files/{video_id}", s.headFile)
		r.Put("/files/{video_id}", s.putFile(log))
		r.Put("/thumbs/{video_id}", s.putThumb(log))
	})
	return r
}

type workerKey struct{}

func videoParam(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := chi.URLParam(r, "video_id")
	if !ValidVideoID(id) {
		http.Error(w, "bad video id", http.StatusBadRequest)
		return "", false
	}
	return id, true
}

func (s *Store) headFile(w http.ResponseWriter, r *http.Request) {
	id, ok := videoParam(w, r)
	if !ok {
		return
	}
	n, ok := s.Received(id)
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Length", strconv.FormatInt(n, 10))
	w.WriteHeader(http.StatusOK)
}

func (s *Store) putFile(log *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := videoParam(w, r)
		if !ok {
			return
		}
		from, total, err := parseRange(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		complete, err := s.Append(id, from, total, r.Body)
		switch {
		case errors.Is(err, ErrOffset):
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		case errs.Is(err, errs.TooLarge):
			http.Error(w, "too large", http.StatusRequestEntityTooLarge)
			return
		case errs.Is(err, errs.InvalidRequest):
			http.Error(w, "bad range", http.StatusBadRequest)
			return
		case err != nil:
			log.Error("store upload", "video_id", id, "err", err)
			http.Error(w, "store failed", http.StatusInternalServerError)
			return
		}
		if complete {
			log.Info("file received", "video_id", id, "bytes", total, "worker_id", r.Context().Value(workerKey{}))
			if err := s.Evict(r.Context()); err != nil {
				log.Warn("evict after upload", "err", err)
			}
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Store) putThumb(log *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := videoParam(w, r)
		if !ok {
			return
		}
		err := s.PutThumb(r.Context(), id, r.Body)
		switch {
		case errs.Is(err, errs.TooLarge):
			http.Error(w, "too large", http.StatusRequestEntityTooLarge)
		case errs.Is(err, errs.InvalidRequest):
			http.Error(w, "not a jpeg", http.StatusBadRequest)
		case err != nil:
			log.Error("store thumbnail", "video_id", id, "err", err)
			http.Error(w, "store failed", http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}
}

// parseRange reads "Content-Range: bytes <from>-<to>/<total>"; without the
// header the body is the whole file.
func parseRange(r *http.Request) (from, total int64, err error) {
	cr := r.Header.Get("Content-Range")
	if cr == "" {
		if r.ContentLength <= 0 {
			return 0, 0, errors.New("Content-Length or Content-Range required")
		}
		return 0, r.ContentLength, nil
	}
	var to int64
	if _, err := fmt.Sscanf(cr, "bytes %d-%d/%d", &from, &to, &total); err != nil {
		return 0, 0, fmt.Errorf("bad Content-Range %q", cr)
	}
	if from < 0 || to < from || to >= total {
		return 0, 0, fmt.Errorf("bad Content-Range %q", cr)
	}
	return from, total, nil
}
