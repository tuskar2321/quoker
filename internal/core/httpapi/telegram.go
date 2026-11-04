package httpapi

import (
	"net/http"

	"github.com/tuskar2321/quoker/internal/core/bot"
)

type linkTokenResponse struct {
	DeepLink  string `json:"deep_link"`
	ExpiresAt string `json:"expires_at"`
}

type linkStatusResponse struct {
	Status    bot.LinkStatus `json:"status"`
	Username  *string        `json:"username,omitempty"`
	FirstName *string        `json:"first_name,omitempty"`
}

// createTelegramLink handles POST /v1/auth/telegram/link.
func (s *server) createTelegramLink(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	info, err := s.bot.CreateLinkToken(r.Context(), p.UserID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, linkTokenResponse{
		DeepLink:  info.DeepLink,
		ExpiresAt: info.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z"),
	})
}

// telegramLinkStatus handles GET /v1/auth/telegram/status.
func (s *server) telegramLinkStatus(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	info, err := s.bot.TelegramLinkStatus(r.Context(), p.UserID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	resp := linkStatusResponse{Status: info.Status}
	if info.Username != "" {
		resp.Username = &info.Username
	}
	if info.FirstName != "" {
		resp.FirstName = &info.FirstName
	}
	writeJSON(w, http.StatusOK, resp)
}

// unlinkTelegram handles DELETE /v1/auth/telegram.
func (s *server) unlinkTelegram(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	if err := s.bot.UnlinkTelegram(r.Context(), p.UserID); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
