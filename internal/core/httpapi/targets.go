package httpapi

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/tuskar2321/quoker/internal/core/targets"
	"github.com/tuskar2321/quoker/internal/shared/errs"
)

type targetDTO struct {
	ID             int64   `json:"id"`
	Title          string  `json:"title"`
	Type           string  `json:"type"`
	Status         string  `json:"status"`
	InactiveReason *string `json:"inactive_reason"`
	Hidden         bool    `json:"hidden"`
	LastUsedAt     *string `json:"last_used_at"`
}

func toTargetDTO(t targets.Target) targetDTO {
	d := targetDTO{
		ID:     t.ID,
		Title:  t.Title,
		Type:   t.Type,
		Status: string(t.Status),
		Hidden: t.Hidden,
	}
	if t.InactiveReason != "" {
		r := string(t.InactiveReason)
		d.InactiveReason = &r
	}
	if !t.LastUsedAt.IsZero() {
		d.LastUsedAt = timeDTO(t.LastUsedAt)
	}
	return d
}

// listTargets handles GET /v1/targets.
func (s *server) listTargets(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	includeHidden := r.URL.Query().Get("include_hidden") == "true"

	items, err := s.targets.List(r.Context(), p.UserID, includeHidden)
	if err != nil {
		writeError(w, r, err)
		return
	}
	dtos := make([]targetDTO, len(items))
	for i, t := range items {
		dtos[i] = toTargetDTO(t)
	}
	writeJSON(w, http.StatusOK, struct {
		Items []targetDTO `json:"items"`
	}{dtos})
}

// patchTarget handles PATCH /v1/targets/{id}.
func (s *server) patchTarget(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, r, errs.New(errs.InvalidRequest, "invalid target id"))
		return
	}
	var req struct {
		Hidden *bool `json:"hidden"`
	}
	if err := decodeJSON(r, &req, false); err != nil {
		writeError(w, r, err)
		return
	}
	if req.Hidden == nil {
		writeError(w, r, errs.New(errs.InvalidRequest, "hidden field required"))
		return
	}

	p := principalFrom(r.Context())
	t, err := s.targets.SetHidden(r.Context(), p.UserID, id, *req.Hidden)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Target targetDTO `json:"target"`
	}{toTargetDTO(t)})
}
