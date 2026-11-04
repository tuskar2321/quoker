package httpapi

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/tuskar2321/quoker/internal/core/accounts"
	"github.com/tuskar2321/quoker/internal/shared/errs"
)

const (
	defaultInviteDays = 7
	maxInviteDays     = 30
)

type inviteDTO struct {
	ID         int64   `json:"id"`
	Role       string  `json:"role"`
	CodePrefix string  `json:"code_prefix"`
	CreatedAt  *string `json:"created_at"`
	ExpiresAt  *string `json:"expires_at"`
	UsedBy     *int64  `json:"used_by"`
	UsedAt     *string `json:"used_at"`
}

type adminUserDTO struct {
	ID                int64   `json:"id"`
	Role              string  `json:"role"`
	Status            string  `json:"status"`
	TelegramUsername  *string `json:"telegram_username"`
	TelegramFirstName *string `json:"telegram_first_name"`
	TargetsCount      int     `json:"targets_count"`
	CreatedAt         *string `json:"created_at"`
	LastSeenAt        *string `json:"last_seen_at"`
}

type itemsDTO[T any] struct {
	Items []T `json:"items"`
}

func (s *server) listInvites(w http.ResponseWriter, r *http.Request) {
	invs, err := s.acc.ListInvites(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := itemsDTO[inviteDTO]{Items: make([]inviteDTO, 0, len(invs))}
	for _, inv := range invs {
		out.Items = append(out.Items, inviteDTO{
			ID:         inv.ID,
			Role:       string(inv.Role),
			CodePrefix: inv.CodePrefix,
			CreatedAt:  timeDTO(inv.CreatedAt),
			ExpiresAt:  timeDTO(inv.ExpiresAt),
			UsedBy:     intDTO(inv.UsedBy),
			UsedAt:     timeDTO(inv.UsedAt),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *server) createInvite(w http.ResponseWriter, r *http.Request) {
	req := struct {
		Role          string `json:"role"`
		ExpiresInDays int    `json:"expires_in_days"`
	}{Role: string(accounts.RoleUser), ExpiresInDays: defaultInviteDays}
	if err := decodeJSON(r, &req, true); err != nil {
		writeError(w, r, err)
		return
	}
	role, err := accounts.ParseRole(req.Role)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if req.ExpiresInDays < 1 || req.ExpiresInDays > maxInviteDays {
		writeError(w, r, errs.New(errs.InvalidRequest, "expires_in_days out of range"))
		return
	}
	code, expires, err := s.acc.CreateInvite(r.Context(), principalFrom(r.Context()).UserID, role,
		time.Duration(req.ExpiresInDays)*24*time.Hour)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, struct {
		Code      string  `json:"code"`
		ExpiresAt *string `json:"expires_at"`
	}{code, timeDTO(expires)})
}

func (s *server) revokeInvite(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "invite_id")
	if !ok {
		writeError(w, r, errs.New(errs.InvalidRequest, "bad invite id"))
		return
	}
	if err := s.acc.RevokeInvite(r.Context(), id); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) listUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.acc.ListUsers(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := itemsDTO[adminUserDTO]{Items: make([]adminUserDTO, 0, len(users))}
	for _, u := range users {
		out.Items = append(out.Items, adminUserDTO{
			ID:                u.ID,
			Role:              string(u.Role),
			Status:            string(u.Status),
			TelegramUsername:  strDTO(u.TelegramUsername),
			TelegramFirstName: strDTO(u.TelegramFirstName),
			TargetsCount:      u.TargetsCount,
			CreatedAt:         timeDTO(u.CreatedAt),
			LastSeenAt:        timeDTO(u.LastSeenAt),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *server) blockUser(w http.ResponseWriter, r *http.Request) {
	s.withUserID(w, r, func(id int64) error {
		return s.acc.Block(r.Context(), principalFrom(r.Context()).UserID, id)
	})
}

func (s *server) unblockUser(w http.ResponseWriter, r *http.Request) {
	s.withUserID(w, r, func(id int64) error { return s.acc.Unblock(r.Context(), id) })
}

func (s *server) withUserID(w http.ResponseWriter, r *http.Request, fn func(int64) error) {
	id, ok := pathID(r, "user_id")
	if !ok {
		writeError(w, r, errs.New(errs.InvalidRequest, "bad user id"))
		return
	}
	if err := fn(id); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func pathID(r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, name), 10, 64)
	return id, err == nil && id > 0
}
