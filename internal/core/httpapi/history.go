package httpapi

import (
	"net/http"
	"strconv"

	"github.com/tuskar2321/quoker/internal/core/history"
	"github.com/tuskar2321/quoker/internal/shared/errs"
)

type historyActionDTO struct {
	Type        string  `json:"type"`
	TargetTitle *string `json:"target_title"`
	SendID      *int64  `json:"send_id"`
	At          *string `json:"at"`
}

type historyItemDTO struct {
	ID      int64              `json:"id"`
	Video   *videoDTO          `json:"video"`
	Actions []historyActionDTO `json:"actions"`
}

// listHistory handles GET /v1/history.
func (s *server) listHistory(w http.ResponseWriter, r *http.Request) {
	limit := history.DefaultLimit
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > history.MaxLimit {
			writeError(w, r, errs.New(errs.InvalidRequest, "limit must be 1..50"))
			return
		}
		limit = n
	}
	page, err := s.history.List(r.Context(), principalFrom(r.Context()).UserID, r.URL.Query().Get("cursor"), limit)
	if err != nil {
		writeError(w, r, err)
		return
	}
	items := make([]historyItemDTO, len(page.Items))
	for i, it := range page.Items {
		d := historyItemDTO{ID: it.ID, Video: toVideoDTO(it.Video), Actions: make([]historyActionDTO, len(it.Actions))}
		for j, a := range it.Actions {
			d.Actions[j] = historyActionDTO{Type: a.Type, TargetTitle: strDTO(a.TargetTitle), SendID: intDTO(a.SendID), At: timeDTO(a.At)}
		}
		items[i] = d
	}
	writeJSON(w, http.StatusOK, struct {
		Items      []historyItemDTO `json:"items"`
		NextCursor *string          `json:"next_cursor"`
	}{items, strDTO(page.NextCursor)})
}

// recordResolved adds a "resolved" history entry; history is best effort and
// never fails the request that produced the video.
func (s *server) recordResolved(r *http.Request, videoID string) {
	if s.history == nil {
		return
	}
	if err := s.history.RecordResolved(r.Context(), principalFrom(r.Context()).UserID, videoID); err != nil {
		loggerFrom(r.Context()).Warn("record resolved", "video_id", videoID, "err", err)
	}
}
