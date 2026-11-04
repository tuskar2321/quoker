package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/tuskar2321/quoker/internal/core/videos"
	"github.com/tuskar2321/quoker/internal/shared/errs"
)

// resolveRequest is the body for POST /v1/videos/resolve.
type resolveRequest struct {
	URL string `json:"url"`
}

// videoDTO is the shared representation of a Video across all endpoints.
type videoDTO struct {
	ID           string  `json:"id"`
	Platform     string  `json:"platform"`
	CanonicalURL string  `json:"canonical_url"`
	Author       *string `json:"author,omitempty"`
	Caption      *string `json:"caption,omitempty"`
	DurationS    *int64  `json:"duration_s,omitempty"`
	SizeBytes    *int64  `json:"size_bytes,omitempty"`
	Width        *int64  `json:"width,omitempty"`
	Height       *int64  `json:"height,omitempty"`
	NoWatermark  bool    `json:"no_watermark"`
	HasThumbnail bool    `json:"has_thumbnail"`
	FileStatus   string  `json:"file_status"`
}

type resolveResponse struct {
	Video  *videoDTO `json:"video,omitempty"`
	TaskID *string   `json:"task_id,omitempty"`
}

type taskResponse struct {
	ID       string    `json:"id"`
	Kind     string    `json:"kind"`
	Status   string    `json:"status"`
	Progress int       `json:"progress"`
	Video    *videoDTO `json:"video,omitempty"`
	ErrCode  *string   `json:"error_code,omitempty"`
}

// toVideoDTO converts a videos.Video to its wire form.
func toVideoDTO(v *videos.Video) *videoDTO {
	return &videoDTO{
		ID:           v.ID,
		Platform:     v.Platform,
		CanonicalURL: v.CanonicalURL,
		Author:       strDTO(v.Author),
		Caption:      strDTO(v.Caption),
		DurationS:    intDTO(int64(v.DurationS)),
		SizeBytes:    intDTO(v.SizeBytes),
		Width:        intDTO(int64(v.Width)),
		Height:       intDTO(int64(v.Height)),
		NoWatermark:  v.NoWatermark,
		HasThumbnail: v.HasThumbnail,
		FileStatus:   string(v.FileStatus),
	}
}

// resolveVideo handles POST /v1/videos/resolve.
// Returns 200 with the video if resolved within resolveWait, or 202 with task_id.
func (s *server) resolveVideo(w http.ResponseWriter, r *http.Request) {
	var req resolveRequest
	if err := decodeJSON(r, &req, false); err != nil {
		writeError(w, r, err)
		return
	}
	if req.URL == "" {
		writeError(w, r, errs.New(errs.InvalidRequest, "url is required"))
		return
	}

	p := principalFrom(r.Context())
	res, err := s.videos.Resolve(r.Context(), req.URL, p.UserID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if res.Video != nil {
		writeJSON(w, http.StatusOK, resolveResponse{Video: toVideoDTO(res.Video)})
		return
	}
	writeJSON(w, http.StatusAccepted, resolveResponse{TaskID: &res.TaskID})
}

// getVideo handles GET /v1/videos/{video_id}.
func (s *server) getVideo(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "video_id")
	v, err := s.videos.Get(r.Context(), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, resolveResponse{Video: toVideoDTO(v)})
}

// getTask handles GET /v1/tasks/{task_id}.
func (s *server) getTask(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "task_id")
	task, v, err := s.videos.GetTask(r.Context(), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	resp := taskResponse{
		ID:       task.ID,
		Kind:     task.Kind,
		Status:   task.Status,
		Progress: task.Progress,
		ErrCode:  strDTO(task.ErrCode),
	}
	if v != nil {
		resp.Video = toVideoDTO(v)
	}
	writeJSON(w, http.StatusOK, resp)
}
