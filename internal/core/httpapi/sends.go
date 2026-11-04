package httpapi

import (
	"net/http"
	"strconv"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	"github.com/tuskar2321/quoker/internal/core/sends"
	"github.com/tuskar2321/quoker/internal/shared/errs"
)

type sendDTO struct {
	ID        int64     `json:"id"`
	VideoID   string    `json:"video_id"`
	TargetID  int64     `json:"target_id"`
	Status    string    `json:"status"`
	Error     *errorDTO `json:"error"`
	CreatedAt *string   `json:"created_at"`
	UpdatedAt *string   `json:"updated_at"`
}

func toSendDTO(s *sends.Send) sendDTO {
	d := sendDTO{
		ID:        s.ID,
		VideoID:   s.VideoID,
		TargetID:  s.TargetID,
		Status:    string(s.Status),
		CreatedAt: timeDTO(s.CreatedAt),
		UpdatedAt: timeDTO(s.UpdatedAt),
	}
	if s.ErrorCode != "" {
		e := toErrorDTO(errs.Code(s.ErrorCode))
		d.Error = &e
	}
	return d
}

type sendResponse struct {
	Send sendDTO `json:"send"`
}

// createSend handles POST /v1/sends.
func (s *server) createSend(w http.ResponseWriter, r *http.Request) {
	key := r.Header.Get("Idempotency-Key")
	if key == "" || len(key) > 64 {
		writeError(w, r, errs.New(errs.InvalidRequest, "Idempotency-Key header is required"))
		return
	}
	var req struct {
		VideoID    string  `json:"video_id"`
		TargetID   int64   `json:"target_id"`
		Caption    *string `json:"caption"`
		SignAsUser bool    `json:"sign_as_user"`
	}
	if err := decodeJSON(r, &req, false); err != nil {
		writeError(w, r, err)
		return
	}
	switch {
	case req.VideoID == "":
		writeError(w, r, errs.New(errs.InvalidRequest, "video_id is required"))
		return
	case req.TargetID <= 0:
		writeError(w, r, errs.New(errs.InvalidRequest, "target_id is required"))
		return
	case req.Caption != nil && utf8.RuneCountInString(*req.Caption) > sends.CaptionMaxRunes:
		writeError(w, r, errs.New(errs.InvalidRequest, "caption too long"))
		return
	}

	p := principalFrom(r.Context())
	usr, err := s.acc.User(r.Context(), p.UserID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if !usr.TelegramLinked() {
		writeError(w, r, errs.New(errs.TelegramNotLinked, "telegram is not linked"))
		return
	}
	if ok, retry := s.sendLimit.Allow(strconv.FormatInt(p.UserID, 10)); !ok {
		writeRateLimited(w, r, retry)
		return
	}

	snd, err := s.sends.Create(r.Context(), sends.CreateParams{
		VideoID:           req.VideoID,
		TargetID:          req.TargetID,
		Caption:           req.Caption,
		SignAsUser:        req.SignAsUser,
		IdempotencyKey:    key,
		CallerID:          p.UserID,
		TelegramUserID:    usr.TelegramUserID,
		TelegramUsername:  usr.TelegramUsername,
		TelegramFirstName: usr.TelegramFirstName,
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, sendResponse{toSendDTO(snd)})
}

// getSend handles GET /v1/sends/{id}.
func (s *server) getSend(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, r, errs.New(errs.InvalidRequest, "invalid send id"))
		return
	}
	snd, err := s.sends.Get(r.Context(), principalFrom(r.Context()).UserID, id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, sendResponse{toSendDTO(snd)})
}
