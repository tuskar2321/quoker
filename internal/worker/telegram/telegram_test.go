package telegram_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tuskar2321/quoker/internal/shared/errs"
	"github.com/tuskar2321/quoker/internal/worker/telegram"
)

const token = "123456:TEST-TOKEN-NOT-REAL"

func TestSendVideo(t *testing.T) {
	var got struct {
		fields    map[string]string
		video     int
		thumb     string
		length    int64
		pathToken bool
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.pathToken = r.URL.Path == "/bot"+token+"/sendVideo"
		got.length = r.ContentLength
		if err := r.ParseMultipartForm(1 << 20); err != nil { //nolint:gosec // test server
			t.Errorf("parse form: %v", err)
		}
		got.fields = map[string]string{}
		for k, v := range r.MultipartForm.Value {
			got.fields[k] = v[0]
		}
		if fh := r.MultipartForm.File["video"]; len(fh) == 1 {
			got.video = int(fh[0].Size)
		}
		if fh := r.MultipartForm.File["thumb"]; len(fh) == 1 {
			f, _ := fh[0].Open()
			b, _ := io.ReadAll(f)
			got.thumb = string(b)
		}
		_, _ = io.WriteString(w, `{"ok":true,"result":{"message_id":99}}`)
	}))
	defer srv.Close()

	path := filepath.Join(t.TempDir(), "v.mp4")
	if err := os.WriteFile(path, make([]byte, 5000), 0o600); err != nil {
		t.Fatal(err)
	}
	var lastSent int64
	id, err := telegram.New(nil).SendVideo(context.Background(), srv.URL, token, telegram.Video{
		ChatID: -100, CaptionHTML: "a &amp; b", Path: path, Thumb: []byte("JPEG"),
		DurationS: 12, Width: 720, Height: 1280,
		Progress: func(sent, _ int64) { lastSent = sent },
	})
	if err != nil {
		t.Fatal(err)
	}
	if id != 99 || !got.pathToken || got.video != 5000 || got.thumb != "JPEG" || lastSent != 5000 {
		t.Fatalf("id=%d got=%+v lastSent=%d", id, got, lastSent)
	}
	want := map[string]string{
		"chat_id": "-100", "caption": "a &amp; b", "parse_mode": "HTML", "supports_streaming": "true",
		"duration": "12", "width": "720", "height": "1280", "thumbnail": "attach://thumb",
	}
	for k, v := range want {
		if got.fields[k] != v {
			t.Errorf("field %s = %q, want %q", k, got.fields[k], v)
		}
	}
	if got.length <= 5000 {
		t.Errorf("Content-Length = %d, want explicit length", got.length)
	}
}

func TestSendVideo_noCaption(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseMultipartForm(1 << 20) //nolint:gosec // test server
		if _, ok := r.MultipartForm.Value["caption"]; ok {
			t.Error("caption sent for empty CaptionHTML")
		}
		if _, ok := r.MultipartForm.Value["parse_mode"]; ok {
			t.Error("parse_mode sent without caption")
		}
		_, _ = io.WriteString(w, `{"ok":true,"result":{"message_id":1}}`)
	}))
	defer srv.Close()
	path := filepath.Join(t.TempDir(), "v.mp4")
	_ = os.WriteFile(path, []byte("x"), 0o600)
	if _, err := telegram.New(nil).SendVideo(context.Background(), srv.URL, token, telegram.Video{ChatID: 1, Path: path}); err != nil {
		t.Fatal(err)
	}
}

func TestErrors(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   errs.Code
	}{
		{"flood", 429, `{"ok":false,"error_code":429,"description":"Too Many Requests: retry after 7","parameters":{"retry_after":7}}`, errs.RateLimited},
		{"kicked", 403, `{"ok":false,"error_code":403,"description":"Forbidden: bot was kicked from the group chat"}`, errs.TargetForbidden},
		{"chat not found", 400, `{"ok":false,"error_code":400,"description":"Bad Request: chat not found"}`, errs.TargetInactive},
		{"too big", 413, `{"ok":false,"error_code":413,"description":"Request Entity Too Large"}`, errs.TooLarge},
		{"bad token", 401, `{"ok":false,"error_code":401,"description":"Unauthorized"}`, errs.Internal},
		{"gateway html", 502, `<html>bad gateway</html>`, errs.UpstreamError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = io.WriteString(w, tt.body)
			}))
			defer srv.Close()
			_, err := telegram.New(nil).Call(context.Background(), srv.URL, token, "getMe", nil)
			var te *telegram.Error
			if !asError(err, &te) || te.Code() != tt.want {
				t.Fatalf("err = %v, want code %s", err, tt.want)
			}
			if tt.status == 429 && te.RetryAfter != 7 {
				t.Fatalf("RetryAfter = %d", te.RetryAfter)
			}
		})
	}
}

func TestCall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p map[string]any
		_ = json.NewDecoder(r.Body).Decode(&p)
		if r.Header.Get("Content-Type") != "application/json" || p["chat_id"] != float64(5) {
			t.Errorf("bad request: %v %v", r.Header, p)
		}
		_, _ = io.WriteString(w, `{"ok":true,"result":{"status":"administrator"}}`)
	}))
	defer srv.Close()
	res, err := telegram.New(nil).Call(context.Background(), srv.URL, token, "getChatMember", []byte(`{"chat_id":5}`))
	if err != nil || string(res) != `{"status":"administrator"}` {
		t.Fatalf("res = %s, err = %v", res, err)
	}
}

func TestTransportErrorHidesToken(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	base := srv.URL
	srv.Close() // connection refused
	_, err := telegram.New(nil).Call(context.Background(), base, token, "getMe", nil)
	if !errs.Is(err, errs.UpstreamError) {
		t.Fatalf("err = %v, want upstream_error", err)
	}
	if strings.Contains(err.Error(), "TEST-TOKEN") {
		t.Fatalf("token leaked into error: %v", err)
	}
}

func TestMissingToken(t *testing.T) {
	_, err := telegram.New(nil).Call(context.Background(), "http://127.0.0.1:1", "", "getMe", nil)
	if !errs.Is(err, errs.Internal) {
		t.Fatalf("err = %v", err)
	}
}

func asError(err error, target **telegram.Error) bool {
	return errors.As(err, target)
}
