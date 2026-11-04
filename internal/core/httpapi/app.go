package httpapi

import (
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/tuskar2321/quoker/internal/core/settings"
)

// appVersion handles GET /v1/app/version (no session, no version check).
func (s *server) appVersion(w http.ResponseWriter, r *http.Request) {
	rel, err := s.releases.Latest(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	minCode, err := s.settings.Int(r.Context(), settings.MinSupportedVersionCode, 0)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		VersionCode             int64  `json:"version_code"`
		VersionName             string `json:"version_name"`
		APKURL                  string `json:"apk_url"`
		SHA256                  string `json:"sha256"`
		MinSupportedVersionCode int64  `json:"min_supported_version_code"`
		Changelog               string `json:"changelog"`
	}{rel.VersionCode, rel.VersionName, s.baseURL(r) + "/download/" + rel.FileName, rel.SHA256, minCode, rel.Changelog})
}

// baseURL is the configured public origin, or the one the request came to.
func (s *server) baseURL(r *http.Request) string {
	if s.publicURL != "" {
		return s.publicURL
	}
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

// downloadAPK handles GET /download/quoker-<version_name>.apk. Only
// registered releases are served; Range lets the app resume.
func (s *server) downloadAPK(w http.ResponseWriter, r *http.Request) {
	f, rel, err := s.releases.Open(r.Context(), chi.URLParam(r, "file"))
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
	w.Header().Set("Content-Type", "application/vnd.android.package-archive")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, rel.FileName))
	w.Header().Set("ETag", `"`+rel.SHA256+`"`)
	http.ServeContent(w, r, "", st.ModTime(), f)
}
