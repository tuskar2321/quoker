package httpapi

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"

	"github.com/tuskar2321/quoker/internal/core/videos"
	"github.com/tuskar2321/quoker/internal/shared/errs"
)

// fileDTO is the FileState schema.
type fileDTO struct {
	Status    string `json:"status"`
	SizeBytes *int64 `json:"size_bytes"`
}

// videoDTO is the Video schema.
type videoDTO struct {
	ID           string  `json:"id"`
	Platform     string  `json:"platform"`
	SourceURL    string  `json:"source_url"`
	CanonicalURL string  `json:"canonical_url"`
	Author       *string `json:"author"`
	Caption      *string `json:"caption"`
	DurationS    *int64  `json:"duration_s"`
	SizeBytes    *int64  `json:"size_bytes"`
	Width        *int64  `json:"width"`
	Height       *int64  `json:"height"`
	NoWatermark  *bool   `json:"no_watermark"`
	ThumbnailURL *string `json:"thumbnail_url"`
	File         fileDTO `json:"file"`
}

func toVideoDTO(v *videos.Video) *videoDTO {
	d := &videoDTO{
		ID:           v.ID,
		Platform:     v.Platform,
		SourceURL:    v.SourceURL,
		CanonicalURL: v.CanonicalURL,
		Author:       strDTO(v.Author),
		Caption:      strDTO(v.Caption),
		DurationS:    intDTO(int64(v.DurationS)),
		SizeBytes:    intDTO(v.SizeBytes),
		Width:        intDTO(int64(v.Width)),
		Height:       intDTO(int64(v.Height)),
		File:         fileDTO{Status: string(v.FileStatus)},
	}
	if d.SourceURL == "" {
		d.SourceURL = v.CanonicalURL
	}
	if v.Platform == "tiktok" {
		d.NoWatermark = &v.NoWatermark
	}
	if v.HasThumbnail {
		u := "/v1/videos/" + url.PathEscape(v.ID) + "/thumbnail"
		d.ThumbnailURL = &u
	}
	if v.FileStatus == videos.FileReady {
		d.File.SizeBytes = intDTO(v.FileSize)
	}
	return d
}

type videoResponse struct {
	Video *videoDTO `json:"video"`
}

type taskIDResponse struct {
	TaskID string `json:"task_id"`
}

// taskDTO is the Task schema; internal queue states are folded into it.
type taskDTO struct {
	ID       string    `json:"id"`
	Kind     string    `json:"kind"`
	Status   string    `json:"status"`
	Progress *int      `json:"progress"`
	Video    *videoDTO `json:"video"`
	Error    *errorDTO `json:"error"`
}

func publicTaskStatus(s string) string {
	switch s {
	case "leased":
		return "running"
	case "cancelled":
		return "failed"
	}
	return s
}

// resolveVideo handles POST /v1/videos/resolve: 200 with the video when it
// resolves within the wait, otherwise 202 with the task to poll.
func (s *server) resolveVideo(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL string `json:"url"`
	}
	if err := decodeJSON(r, &req, false); err != nil {
		writeError(w, r, err)
		return
	}
	if req.URL == "" || len(req.URL) > 2048 {
		writeError(w, r, errs.New(errs.InvalidRequest, "url is required, up to 2048 characters"))
		return
	}
	userID := principalFrom(r.Context()).UserID
	if overLimit(w, r, s.resolveLimit, userID, "") {
		return
	}
	res, err := s.videos.Resolve(r.Context(), req.URL, userID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if res.Video != nil {
		s.recordResolved(r, res.Video.ID)
		writeJSON(w, http.StatusOK, videoResponse{toVideoDTO(res.Video)})
		return
	}
	writeJSON(w, http.StatusAccepted, taskIDResponse{res.TaskID})
}

// getVideo handles GET /v1/videos/{video_id}.
func (s *server) getVideo(w http.ResponseWriter, r *http.Request) {
	v, err := s.videos.Get(r.Context(), chi.URLParam(r, "video_id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, videoResponse{toVideoDTO(v)})
}

// getTask handles GET /v1/tasks/{task_id}.
func (s *server) getTask(w http.ResponseWriter, r *http.Request) {
	task, v, err := s.videos.GetTask(r.Context(), chi.URLParam(r, "task_id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	// Only the tasks the API hands out are visible; sends and bot calls
	// have their own endpoints or none.
	if task.Kind != "resolve" && task.Kind != "download" {
		writeError(w, r, errs.New(errs.NotFound, "task not found"))
		return
	}
	resp := taskDTO{ID: task.ID, Kind: task.Kind, Status: publicTaskStatus(task.Status)}
	if task.Progress > 0 {
		resp.Progress = &task.Progress
	}
	if v != nil {
		resp.Video = toVideoDTO(v)
		if task.Kind == "resolve" {
			s.recordResolved(r, v.ID)
		}
	}
	if resp.Status == "failed" {
		e := toErrorDTO(errs.CodeOf(task.Err))
		resp.Error = &e
	}
	writeJSON(w, http.StatusOK, resp)
}

// videoThumbnail handles GET /v1/videos/{video_id}/thumbnail.
func (s *server) videoThumbnail(w http.ResponseWriter, r *http.Request) {
	v, err := s.videos.Get(r.Context(), chi.URLParam(r, "video_id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	if !v.HasThumbnail {
		writeError(w, r, errs.New(errs.NotFound, "no thumbnail"))
		return
	}
	f, err := s.files.OpenThumb(v.ID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	defer func() { _ = f.Close() }()
	st, err := f.Stat()
	if err != nil {
		writeError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "private, max-age=86400")
	http.ServeContent(w, r, "", st.ModTime(), f)
}

// prepareVideo handles POST /v1/videos/{video_id}/prepare.
func (s *server) prepareVideo(w http.ResponseWriter, r *http.Request) {
	v, err := s.videos.Get(r.Context(), chi.URLParam(r, "video_id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	if v.FileStatus == videos.FileReady {
		if s.files.HasFile(v.ID) {
			writeJSON(w, http.StatusOK, struct {
				File fileDTO `json:"file"`
			}{fileDTO{Status: string(videos.FileReady), SizeBytes: intDTO(v.FileSize)}})
			return
		}
		if err := s.videos.ResetFile(r.Context(), v.ID); err != nil {
			writeError(w, r, err)
			return
		}
	}
	taskID, err := s.videos.EnqueueDownload(r.Context(), v, principalFrom(r.Context()).UserID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, taskIDResponse{taskID})
}

// videoFile handles GET /v1/videos/{video_id}/file with Range support.
func (s *server) videoFile(w http.ResponseWriter, r *http.Request) {
	v, err := s.videos.Get(r.Context(), chi.URLParam(r, "video_id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	if v.FileStatus != videos.FileReady {
		writeError(w, r, errs.New(errs.FileNotReady, "file is "+string(v.FileStatus)))
		return
	}
	done, ok := s.streams.acquire(principalFrom(r.Context()).SessionID)
	if !ok {
		writeError(w, r, errs.New(errs.RateLimited, "too many parallel downloads"))
		return
	}
	defer done()

	f, release, err := s.files.OpenFile(v.ID)
	if err != nil {
		if errs.Is(err, errs.FileNotReady) {
			_ = s.videos.ResetFile(r.Context(), v.ID)
		}
		writeError(w, r, err)
		return
	}
	defer release()
	defer func() { _ = f.Close() }()
	st, err := f.Stat()
	if err != nil {
		writeError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.mp4"`, strings.ReplaceAll(v.ID, ":", "_")))
	if v.FileSHA256 != "" {
		w.Header().Set("ETag", `"`+v.FileSHA256+`"`)
	}
	http.ServeContent(w, r, "", st.ModTime(), f)
}

// videoDownloaded handles POST /v1/videos/{video_id}/downloaded.
func (s *server) videoDownloaded(w http.ResponseWriter, r *http.Request) {
	if err := s.videos.RecordDownloaded(r.Context(), principalFrom(r.Context()).UserID, chi.URLParam(r, "video_id")); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// streamLimiter caps concurrent downloads per session.
type streamLimiter struct {
	mu     sync.Mutex
	max    int
	active map[int64]int
}

func newStreamLimiter(limit int) *streamLimiter {
	return &streamLimiter{max: limit, active: map[int64]int{}}
}

func (l *streamLimiter) acquire(session int64) (release func(), ok bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.active[session] >= l.max {
		return nil, false
	}
	l.active[session]++
	var once sync.Once
	return func() {
		once.Do(func() {
			l.mu.Lock()
			defer l.mu.Unlock()
			if l.active[session]--; l.active[session] == 0 {
				delete(l.active, session)
			}
		})
	}, true
}
