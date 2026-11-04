package httpapi

import (
	"net/http"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/tuskar2321/quoker/internal/core/accounts"
	"github.com/tuskar2321/quoker/internal/shared/errs"
)

const maxDeviceName = 64

var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

type meDTO struct {
	ID       int64       `json:"id"`
	Role     string      `json:"role"`
	Telegram telegramDTO `json:"telegram"`
}

type telegramDTO struct {
	Linked    bool    `json:"linked"`
	Username  *string `json:"username"`
	FirstName *string `json:"first_name"`
}

func toMe(u accounts.User) meDTO {
	return meDTO{
		ID:   u.ID,
		Role: string(u.Role),
		Telegram: telegramDTO{
			Linked:    u.TelegramLinked(),
			Username:  strDTO(u.TelegramUsername),
			FirstName: strDTO(u.TelegramFirstName),
		},
	}
}

func (s *server) registerUser(w http.ResponseWriter, r *http.Request) {
	// Every attempt counts, successful or not: the limit guards invite codes
	// against guessing.
	if ok, retry := s.register.Allow(clientIP(r, s.trusted)); !ok {
		writeRateLimited(w, r, retry)
		return
	}
	var req struct {
		InviteCode string `json:"invite_code"`
		DeviceID   string `json:"device_id"`
		DeviceName string `json:"device_name"`
	}
	if err := decodeJSON(r, &req, false); err != nil {
		writeError(w, r, err)
		return
	}
	req.DeviceName = strings.TrimSpace(req.DeviceName)
	if !uuidRe.MatchString(req.DeviceID) || req.DeviceName == "" || utf8.RuneCountInString(req.DeviceName) > maxDeviceName {
		writeError(w, r, errs.New(errs.InvalidRequest, "bad device_id or device_name"))
		return
	}
	v, _ := appVersion(r) // validated by requireVersion
	token, u, err := s.acc.Register(r.Context(), accounts.Registration{
		InviteCode: req.InviteCode,
		DeviceID:   strings.ToLower(req.DeviceID),
		DeviceName: req.DeviceName,
		AppVersion: v,
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	infoFrom(r.Context()).userID = u.ID
	writeJSON(w, http.StatusCreated, struct {
		SessionToken string `json:"session_token"`
		User         meDTO  `json:"user"`
	}{token, toMe(u)})
}

func (s *server) logout(w http.ResponseWriter, r *http.Request) {
	if err := s.acc.Logout(r.Context(), principalFrom(r.Context()).SessionID); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) me(w http.ResponseWriter, r *http.Request) {
	u, err := s.acc.User(r.Context(), principalFrom(r.Context()).UserID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toMe(u))
}
