package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"

	"github.com/tuskar2321/quoker/internal/core/accounts"
	"github.com/tuskar2321/quoker/internal/core/metrics"
	"github.com/tuskar2321/quoker/internal/core/proxy"
	"github.com/tuskar2321/quoker/internal/core/settings"
	"github.com/tuskar2321/quoker/internal/shared/errs"
)

// Client headers (docs/03-public-api.md).
const (
	headerVersionCode = "X-Quoker-Version-Code"
	headerRequestID   = "X-Request-Id"
)

type ctxKey int

const (
	keyInfo ctxKey = iota
	keyLogger
	keyPrincipal
)

// requestInfo is filled in by inner handlers and read by the access log.
type requestInfo struct {
	id        string
	userID    int64
	errorCode errs.Code
}

func infoFrom(ctx context.Context) *requestInfo {
	if v, ok := ctx.Value(keyInfo).(*requestInfo); ok {
		return v
	}
	return &requestInfo{}
}

func loggerFrom(ctx context.Context) *slog.Logger {
	if v, ok := ctx.Value(keyLogger).(*slog.Logger); ok {
		return v
	}
	return slog.Default()
}

func principalFrom(ctx context.Context) accounts.Principal {
	p, _ := ctx.Value(keyPrincipal).(accounts.Principal)
	return p
}

// accessLog assigns a request id, a request-scoped logger, and logs one line
// per request. Query strings and bodies are never logged.
func accessLog(base *slog.Logger, responses *metrics.Counter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			info := &requestInfo{id: newRequestID()}
			log := base.With("request_id", info.id)
			ctx := context.WithValue(r.Context(), keyInfo, info)
			ctx = context.WithValue(ctx, keyLogger, log)
			w.Header().Set(headerRequestID, info.id)

			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r.WithContext(ctx))

			responses.Inc(strconv.Itoa(ww.Status()))
			attrs := []any{
				"method", r.Method,
				"path", r.URL.Path,
				"status", ww.Status(),
				"duration_ms", time.Since(start).Milliseconds(),
			}
			if info.userID != 0 {
				attrs = append(attrs, "user_id", info.userID)
			}
			if info.errorCode != "" {
				attrs = append(attrs, "error_code", string(info.errorCode))
			}
			log.InfoContext(ctx, "http request", attrs...)
		})
	}
}

func newRequestID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b) // crypto/rand.Read never fails on supported platforms
	return hex.EncodeToString(b)
}

// recoverer turns a handler panic into a 500 with the public error format.
func recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if err, ok := v.(error); ok && errors.Is(err, http.ErrAbortHandler) {
					panic(v)
				}
				writeError(w, r, fmt.Errorf("panic: %v", v))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func limitBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		next.ServeHTTP(w, r)
	})
}

// appVersion returns the client version code, 0 when the header is absent.
func appVersion(r *http.Request) (int64, error) {
	v := r.Header.Get(headerVersionCode)
	if v == "" {
		return 0, nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n <= 0 {
		return 0, errs.New(errs.InvalidRequest, "bad "+headerVersionCode)
	}
	return n, nil
}

// requireVersion rejects clients older than min_supported_version_code with
// 426. Requests without the header (curl, health checks) pass.
func requireVersion(st *settings.Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			v, err := appVersion(r)
			if err != nil {
				writeError(w, r, err)
				return
			}
			if v != 0 {
				minV, err := st.Int(r.Context(), settings.MinSupportedVersionCode, 0)
				if err != nil {
					writeError(w, r, err)
					return
				}
				if v < minV {
					writeError(w, r, errs.New(errs.UpgradeRequired, "client version below minimum"))
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// requireSession authenticates "Authorization: Bearer <token>".
func requireSession(acc *accounts.Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
			if !ok || token == "" {
				writeError(w, r, errs.New(errs.Unauthorized, "missing bearer token"))
				return
			}
			v, _ := appVersion(r) // validated by requireVersion
			p, err := acc.Authenticate(r.Context(), token, v)
			if err != nil {
				writeError(w, r, err)
				return
			}
			infoFrom(r.Context()).userID = p.UserID
			ctx := context.WithValue(r.Context(), keyPrincipal, p)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !principalFrom(r.Context()).IsAdmin() {
			writeError(w, r, errs.New(errs.Forbidden, "admin role required"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// clientIP returns the caller address. The API port is reachable only from
// Caddy (docs/10-deploy.md), which sets X-Forwarded-For to the real client.
// clientIP is the address the request came from. X-Forwarded-For counts only
// when the direct peer is a trusted proxy (Caddy). Walking it from the right,
// trusted hops are skipped and the first untrusted address is the client;
// anything further left is whatever the client chose to send.
func clientIP(r *http.Request, trusted proxy.Trusted) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	xff := r.Header.Get("X-Forwarded-For")
	if xff == "" || !trusted.Contains(r.RemoteAddr) {
		return host
	}
	parts := strings.Split(xff, ",")
	for i := len(parts) - 1; i >= 0; i-- {
		hop := strings.TrimSpace(parts[i])
		if hop != "" && !trusted.Contains(hop) {
			return hop
		}
	}
	return strings.TrimSpace(parts[0])
}
