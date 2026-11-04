package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/tuskar2321/quoker/internal/shared/errs"
)

// maxBodyBytes caps JSON request bodies (docs/11-nfr-security.md).
const maxBodyBytes = 1 << 20

// statusOf maps error codes to HTTP statuses (docs/03-public-api.md).
var statusOf = map[errs.Code]int{
	errs.Unauthorized:       http.StatusUnauthorized,
	errs.Forbidden:          http.StatusForbidden,
	errs.UserBlocked:        http.StatusForbidden,
	errs.UpgradeRequired:    http.StatusUpgradeRequired,
	errs.InvalidRequest:     http.StatusBadRequest,
	errs.InvalidInvite:      http.StatusBadRequest,
	errs.TelegramNotLinked:  http.StatusConflict,
	errs.LinkTokenExpired:   http.StatusGone,
	errs.UnsupportedURL:     http.StatusUnprocessableEntity,
	errs.UnsupportedContent: http.StatusUnprocessableEntity,
	errs.PrivateVideo:       http.StatusNotFound,
	errs.NotFound:           http.StatusNotFound,
	errs.FileNotReady:       http.StatusConflict,
	errs.TooLarge:           http.StatusUnprocessableEntity,
	errs.TargetInactive:     http.StatusConflict,
	errs.TargetForbidden:    http.StatusForbidden,
	errs.RateLimited:        http.StatusTooManyRequests,
	errs.WorkerUnavailable:  http.StatusServiceUnavailable,
	errs.UpstreamError:      http.StatusBadGateway,
	errs.Internal:           http.StatusInternalServerError,
}

// messageOf holds the user-facing Russian texts; the client may replace them
// with its own strings by code.
var messageOf = map[errs.Code]string{
	errs.Unauthorized:       "Нужно войти заново",
	errs.Forbidden:          "Недостаточно прав",
	errs.UserBlocked:        "Доступ закрыт администратором",
	errs.UpgradeRequired:    "Обновите приложение",
	errs.InvalidRequest:     "Некорректный запрос",
	errs.InvalidInvite:      "Код не подошёл",
	errs.TelegramNotLinked:  "Сначала привяжите Telegram",
	errs.LinkTokenExpired:   "Ссылка устарела, получите новую",
	errs.UnsupportedURL:     "Эта ссылка не поддерживается",
	errs.UnsupportedContent: "Поддерживаются только видео",
	errs.PrivateVideo:       "Видео приватное или удалено",
	errs.NotFound:           "Не найдено",
	errs.FileNotReady:       "Файл ещё готовится",
	errs.TooLarge:           "Видео слишком большое для отправки",
	errs.TargetInactive:     "Чат недоступен для отправки",
	errs.TargetForbidden:    "У вас больше нет прав в этом чате",
	errs.RateLimited:        "Слишком много запросов, попробуйте позже",
	errs.WorkerUnavailable:  "Сервис временно недоступен",
	errs.UpstreamError:      "Платформа ответила ошибкой, попробуйте ещё раз",
	errs.Internal:           "Что-то пошло не так",
}

type errorBody struct {
	Error errorDTO `json:"error"`
}

type errorDTO struct {
	Code      errs.Code `json:"code"`
	Message   string    `json:"message"`
	Retryable bool      `json:"retryable"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError renders err in the public error format. Errors without a
// domain code become "internal" and are logged with their details.
func writeError(w http.ResponseWriter, r *http.Request, err error) {
	code := errs.CodeOf(err)
	status, ok := statusOf[code]
	if !ok { // a worker-only code leaked: never expose it
		code, status = errs.Internal, http.StatusInternalServerError
	}
	info := infoFrom(r.Context())
	info.errorCode = code
	if status >= http.StatusInternalServerError {
		loggerFrom(r.Context()).ErrorContext(r.Context(), "request failed", "error", err.Error())
	}
	writeJSON(w, status, errorBody{Error: errorDTO{Code: code, Message: messageOf[code], Retryable: code.Retryable()}})
}

func writeRateLimited(w http.ResponseWriter, r *http.Request, retryAfter time.Duration) {
	w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(retryAfter.Seconds()))))
	writeError(w, r, errs.New(errs.RateLimited, "rate limit exceeded"))
}

// decodeJSON reads a single JSON object from the body into v. An empty body
// leaves v untouched when allowEmpty is set.
func decodeJSON(r *http.Request, v any, allowEmpty bool) error {
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(v); err != nil {
		if allowEmpty && errors.Is(err, io.EOF) {
			return nil
		}
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			return errs.New(errs.InvalidRequest, "body too large")
		}
		return errs.Wrap(errs.InvalidRequest, fmt.Errorf("decode body: %w", err))
	}
	if dec.More() {
		return errs.New(errs.InvalidRequest, "trailing data after JSON object")
	}
	return nil
}

// timeDTO renders t as RFC 3339 UTC, or null when zero.
func timeDTO(t time.Time) *string {
	if t.IsZero() {
		return nil
	}
	s := t.UTC().Format(time.RFC3339)
	return &s
}

func strDTO(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func intDTO(n int64) *int64 {
	if n == 0 {
		return nil
	}
	return &n
}
