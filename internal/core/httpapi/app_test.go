package httpapi

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tuskar2321/quoker/internal/core/accounts"
	"github.com/tuskar2321/quoker/internal/core/settings"
)

func TestAppVersionAndDownload(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if r := e.do(req{method: http.MethodGet, path: "/v1/app/version"}); r.status != http.StatusNotFound {
		t.Fatalf("no release: %d %s", r.status, r.raw)
	}

	src := filepath.Join(t.TempDir(), "app.apk")
	body := "PK\x03\x04" + strings.Repeat("a", 100)
	if err := os.WriteFile(src, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	rel, err := e.releases.Add(ctx, 42, "1.4.0", src, "Исправления")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.settings.Set(ctx, settings.MinSupportedVersionCode, "40"); err != nil {
		t.Fatal(err)
	}

	// An app below the minimum is turned away everywhere except here.
	old := map[string]string{"X-Quoker-Version-Code": "30"}
	tok, _ := e.user(accounts.RoleUser)
	if r := e.do(req{method: http.MethodGet, path: "/v1/me", token: tok, header: old}); r.errCode() != "upgrade_required" {
		t.Fatalf("/v1/me for old app: %d %s", r.status, r.raw)
	}
	v := e.do(req{method: http.MethodGet, path: "/v1/app/version", header: old})
	wantURL := e.srv.URL + "/download/quoker-1.4.0.apk"
	if v.status != http.StatusOK || v.body["version_code"] != float64(42) || v.body["version_name"] != "1.4.0" ||
		v.body["apk_url"] != wantURL || v.body["sha256"] != rel.SHA256 ||
		v.body["min_supported_version_code"] != float64(40) || v.body["changelog"] != "Исправления" {
		t.Fatalf("version = %s", v.raw)
	}

	part := e.do(req{method: http.MethodGet, path: "/download/quoker-1.4.0.apk", header: map[string]string{"Range": "bytes=0-3"}})
	if part.status != http.StatusPartialContent || part.raw != "PK\x03\x04" ||
		part.header.Get("Content-Type") != "application/vnd.android.package-archive" ||
		part.header.Get("ETag") != `"`+rel.SHA256+`"` {
		t.Fatalf("download: %d %q %v", part.status, part.raw, part.header)
	}
	full := e.do(req{method: http.MethodGet, path: "/download/quoker-1.4.0.apk", header: old})
	if full.status != http.StatusOK || full.raw != body {
		t.Fatalf("full download: %d, %d bytes", full.status, len(full.raw))
	}
	for _, bad := range []string{"/download/quoker-9.9.9.apk", "/download/..%2Fq.db"} {
		if r := e.do(req{method: http.MethodGet, path: bad}); r.status != http.StatusNotFound {
			t.Fatalf("%s: %d", bad, r.status)
		}
	}
}
