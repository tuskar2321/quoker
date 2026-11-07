package faketelegram_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tuskar2321/quoker/internal/devtools/faketelegram"
)

const (
	testToken = "1000001:TEST-NOT-A-SECRET"
	userID    = 42
	groupID   = -100500
)

// fixture is a running fake with its bot API and admin API.
type fixture struct {
	st       *faketelegram.State
	bot      *httptest.Server
	adminSrv *httptest.Server
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	st := faketelegram.NewState(faketelegram.Config{
		Token: testToken, BotID: 1000001, BotUsername: "quoker_test_bot", MaxUploadBytes: 1 << 20,
	})
	srv := faketelegram.NewServer(st, slog.New(slog.DiscardHandler))
	f := &fixture{
		st:       st,
		bot:      httptest.NewServer(srv.BotHandler()),
		adminSrv: httptest.NewServer(srv.AdminHandler()),
	}
	t.Cleanup(f.bot.Close)
	t.Cleanup(f.adminSrv.Close)
	return f
}

// call posts a JSON body to a Bot API method and returns status and decoded envelope.
func (f *fixture) call(t *testing.T, token, method string, body any) (int, map[string]any) {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	res, err := post(t, f.bot.URL+"/bot"+token+"/"+method, "application/json", bytes.NewReader(raw)) //nolint:bodyclose // decodeEnvelope closes it
	if err != nil {
		t.Fatal(err)
	}
	return decodeEnvelope(t, res)
}

func decodeEnvelope(t *testing.T, res *http.Response) (int, map[string]any) {
	t.Helper()
	defer func() { _ = res.Body.Close() }()
	var out map[string]any
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatalf("decode response (status %d): %v", res.StatusCode, err)
	}
	return res.StatusCode, out
}

// admin posts JSON to the admin API.
func (f *fixture) admin(t *testing.T, method, path string, body any) (int, map[string]any) {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequestWithContext(context.Background(), method, f.adminSrv.URL+path, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.DefaultClient.Do(req) //nolint:bodyclose // decodeEnvelope closes it
	if err != nil {
		t.Fatal(err)
	}
	return decodeEnvelope(t, res)
}

func (f *fixture) seedGroup(t *testing.T, botStatus string) {
	t.Helper()
	status, _ := f.admin(t, http.MethodPost, "/_fake/chats", map[string]any{
		"id": groupID, "type": "supergroup", "title": "Friends", "bot_status": botStatus,
		"members": []map[string]any{
			{"user_id": userID, "status": "administrator", "can_post_messages": true},
			{"user_id": 7, "status": "member"},
		},
	})
	if status != http.StatusOK {
		t.Fatalf("seed chat: status %d", status)
	}
}

func multipartVideo(t *testing.T, fields map[string]string, video []byte, thumb []byte) (string, []byte) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range fields {
		if err := mw.WriteField(k, v); err != nil {
			t.Fatal(err)
		}
	}
	if thumb != nil {
		fw, err := mw.CreateFormFile("thumb", "thumb.jpg")
		if err != nil {
			t.Fatal(err)
		}
		_, _ = fw.Write(thumb)
	}
	if video != nil {
		fw, err := mw.CreateFormFile("video", "video.mp4")
		if err != nil {
			t.Fatal(err)
		}
		_, _ = fw.Write(video)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	return mw.FormDataContentType(), buf.Bytes()
}

func TestBotAuthAndRouting(t *testing.T) {
	f := newFixture(t)
	tests := []struct {
		name   string
		path   string
		status int
	}{
		{"wrong token", "/bot999:WRONG/getMe", http.StatusUnauthorized},
		{"no method", "/bot" + testToken, http.StatusUnauthorized},
		{"unknown method", "/bot" + testToken + "/noSuchMethod", http.StatusNotFound},
		{"unknown path", "/nope", http.StatusNotFound},
		{"getMe ok", "/bot" + testToken + "/getMe", http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := post(t, f.bot.URL+tt.path, "application/json", strings.NewReader("{}")) //nolint:bodyclose // decodeEnvelope closes it
			if err != nil {
				t.Fatal(err)
			}
			status, body := decodeEnvelope(t, res)
			if status != tt.status {
				t.Fatalf("status = %d, want %d (%v)", status, tt.status, body)
			}
			if tt.status == http.StatusOK && body["ok"] != true {
				t.Fatalf("ok = %v", body["ok"])
			}
		})
	}
}

func TestGetMeReturnsBotIdentity(t *testing.T) {
	f := newFixture(t)
	_, body := f.call(t, testToken, "getMe", map[string]any{})
	res, _ := body["result"].(map[string]any)
	if res["username"] != "quoker_test_bot" || res["is_bot"] != true {
		t.Fatalf("getMe result = %v", res)
	}
}

func TestGetUpdatesOffsetConfirmation(t *testing.T) {
	tests := []struct {
		name      string
		injected  int
		offsets   []int64 // offsets passed to successive getUpdates calls
		wantCount []int   // number of updates each call returns
	}{
		{"nothing pending returns empty", 0, []int64{0}, []int{0}},
		{"redelivered until confirmed", 2, []int64{0, 0, 3}, []int{2, 2, 0}},
		{"confirming one keeps the rest", 3, []int64{0, 2}, []int{3, 2}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			for i := 0; i < tt.injected; i++ {
				f.admin(t, http.MethodPost, "/_fake/messages", map[string]any{
					"chat_id": userID, "user_id": userID, "text": "hi",
				})
			}
			for i, off := range tt.offsets {
				_, body := f.call(t, testToken, "getUpdates", map[string]any{"offset": off, "timeout": 0})
				got, _ := body["result"].([]any)
				if len(got) != tt.wantCount[i] {
					t.Fatalf("call %d (offset %d): %d updates, want %d", i, off, len(got), tt.wantCount[i])
				}
			}
		})
	}
}

func TestGetUpdatesLongPollWakesOnInject(t *testing.T) {
	f := newFixture(t)
	done := make(chan []any, 1)
	go func() {
		_, body := f.call(t, testToken, "getUpdates", map[string]any{"offset": 0, "timeout": 20})
		got, _ := body["result"].([]any)
		done <- got
	}()
	time.Sleep(100 * time.Millisecond)
	start := time.Now()
	f.admin(t, http.MethodPost, "/_fake/messages", map[string]any{"chat_id": userID, "user_id": userID, "text": "/start link_x"})
	select {
	case got := <-done:
		if len(got) != 1 {
			t.Fatalf("got %d updates, want 1", len(got))
		}
		if elapsed := time.Since(start); elapsed > 5*time.Second {
			t.Fatalf("long poll woke after %v, want prompt delivery", elapsed)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("long poll did not return after an update was injected")
	}
}

func TestGetUpdatesLongPollTimesOut(t *testing.T) {
	f := newFixture(t)
	start := time.Now()
	_, body := f.call(t, testToken, "getUpdates", map[string]any{"offset": 0, "timeout": 1})
	if got, _ := body["result"].([]any); len(got) != 0 {
		t.Fatalf("got %d updates on an empty queue", len(got))
	}
	if d := time.Since(start); d < 900*time.Millisecond {
		t.Fatalf("returned after %v, want the poll to wait about 1s", d)
	}
}

func TestSendMessageTable(t *testing.T) {
	tests := []struct {
		name       string
		botStatus  string // "" means the chat is not registered
		register   bool
		body       map[string]any
		wantStatus int
		wantDesc   string
	}{
		{"delivers to a member group", "member", true, map[string]any{"chat_id": groupID, "text": "hello"}, http.StatusOK, ""},
		{"unknown chat is inactive", "", false, map[string]any{"chat_id": groupID, "text": "hello"}, http.StatusBadRequest, "chat not found"},
		{"bot removed from chat is forbidden", "kicked", true, map[string]any{"chat_id": groupID, "text": "hello"}, http.StatusForbidden, "kicked"},
		{"bot left chat is forbidden", "left", true, map[string]any{"chat_id": groupID, "text": "hello"}, http.StatusForbidden, "not a member"},
		{"empty text is rejected", "member", true, map[string]any{"chat_id": groupID, "text": "  "}, http.StatusBadRequest, "text is empty"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			if tt.register {
				f.seedGroup(t, tt.botStatus)
			}
			status, body := f.call(t, testToken, "sendMessage", tt.body)
			if status != tt.wantStatus {
				t.Fatalf("status = %d, want %d (%v)", status, tt.wantStatus, body)
			}
			if tt.wantDesc != "" && !strings.Contains(body["description"].(string), tt.wantDesc) {
				t.Fatalf("description = %v, want it to mention %q", body["description"], tt.wantDesc)
			}
			sent := f.st.Sent()
			if tt.wantStatus == http.StatusOK && (len(sent) != 1 || sent[0].Text != "hello") {
				t.Fatalf("sent records = %+v", sent)
			}
			if tt.wantStatus != http.StatusOK && len(sent) != 0 {
				t.Fatalf("failed send was recorded: %+v", sent)
			}
		})
	}
}

func TestSendVideoMultipart(t *testing.T) {
	video := bytes.Repeat([]byte("v"), 4096)
	tests := []struct {
		name       string
		video      []byte
		thumb      []byte
		fields     map[string]string
		max        int64
		wantStatus int
	}{
		{"video with caption and thumb", video, []byte("jpeg"), map[string]string{
			"chat_id": "-100500", "caption": "from @someone", "parse_mode": "HTML",
			"duration": "12", "width": "720", "height": "1280", "thumbnail": "attach://thumb",
		}, 1 << 20, http.StatusOK},
		{"video without thumb", video, nil, map[string]string{"chat_id": "-100500"}, 1 << 20, http.StatusOK},
		{"no video part", nil, nil, map[string]string{"chat_id": "-100500"}, 1 << 20, http.StatusBadRequest},
		{"over the upload limit", bytes.Repeat([]byte("x"), 2<<20), nil, map[string]string{"chat_id": "-100500"}, 1 << 20, http.StatusRequestEntityTooLarge},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			f.seedGroup(t, "member")
			ctype, body := multipartVideo(t, tt.fields, tt.video, tt.thumb)
			res, err := post(t, f.bot.URL+"/bot"+testToken+"/sendVideo", ctype, bytes.NewReader(body)) //nolint:bodyclose // decodeEnvelope closes it
			if err != nil {
				t.Fatal(err)
			}
			status, resp := decodeEnvelope(t, res)
			if status != tt.wantStatus {
				t.Fatalf("status = %d, want %d (%v)", status, tt.wantStatus, resp)
			}
			if status != http.StatusOK {
				return
			}
			sent := f.st.Sent()
			if len(sent) != 1 {
				t.Fatalf("sent = %+v", sent)
			}
			got := sent[0]
			if got.Bytes != int64(len(tt.video)) || got.Method != "sendVideo" {
				t.Fatalf("recorded video size %d (method %s), want %d", got.Bytes, got.Method, len(tt.video))
			}
			if got.Text != tt.fields["caption"] || got.Thumb != (tt.thumb != nil) {
				t.Fatalf("recorded caption %q thumb %v", got.Text, got.Thumb)
			}
			msg, _ := resp["result"].(map[string]any)
			if _, ok := msg["video"].(map[string]any); !ok {
				t.Fatalf("result has no video object: %v", msg)
			}
		})
	}
}

func TestGetChatMemberTable(t *testing.T) {
	tests := []struct {
		name        string
		chatID      int64
		userID      int64
		botStatus   string
		wantStatus  int
		wantStatus2 string
	}{
		{"administrator", groupID, userID, "member", http.StatusOK, "administrator"},
		{"member", groupID, 7, "member", http.StatusOK, "member"},
		{"non-member is left, not an error", groupID, 99, "member", http.StatusOK, "left"},
		{"unknown chat", -1, userID, "member", http.StatusBadRequest, ""},
		{"bot kicked from chat", groupID, userID, "kicked", http.StatusForbidden, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			f.seedGroup(t, tt.botStatus)
			status, body := f.call(t, testToken, "getChatMember", map[string]any{"chat_id": tt.chatID, "user_id": tt.userID})
			if status != tt.wantStatus {
				t.Fatalf("status = %d, want %d (%v)", status, tt.wantStatus, body)
			}
			if tt.wantStatus2 == "" {
				return
			}
			res, _ := body["result"].(map[string]any)
			if res["status"] != tt.wantStatus2 {
				t.Fatalf("status field = %v, want %s", res["status"], tt.wantStatus2)
			}
		})
	}
}

func TestGetFileAndDownload(t *testing.T) {
	f := newFixture(t)
	f.admin(t, http.MethodPost, "/_fake/files", map[string]any{"file_id": "cookies1", "content": "name=value"})

	status, body := f.call(t, testToken, "getFile", map[string]any{"file_id": "cookies1"})
	if status != http.StatusOK {
		t.Fatalf("getFile status %d (%v)", status, body)
	}
	res, _ := body["result"].(map[string]any)
	path, _ := res["file_path"].(string)
	if path == "" {
		t.Fatalf("no file_path: %v", res)
	}
	dl, err := get(t, f.bot.URL+"/file/bot"+testToken+"/"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = dl.Body.Close() }()
	b, _ := io.ReadAll(dl.Body)
	if dl.StatusCode != http.StatusOK || string(b) != "name=value" {
		t.Fatalf("download = %d %q", dl.StatusCode, b)
	}

	if status, _ := f.call(t, testToken, "getFile", map[string]any{"file_id": "missing"}); status != http.StatusBadRequest {
		t.Fatalf("getFile unknown id status = %d, want 400", status)
	}
	bad, err := get(t, f.bot.URL+"/file/bot"+testToken+"/documents/missing")
	if err != nil {
		t.Fatal(err)
	}
	_ = bad.Body.Close()
	if bad.StatusCode != http.StatusNotFound {
		t.Fatalf("download unknown file status = %d, want 404", bad.StatusCode)
	}
}

func TestFaultsAreConsumed(t *testing.T) {
	tests := []struct {
		name     string
		fault    map[string]any
		calls    int
		statuses []int
		retry    bool
	}{
		{"429 once with retry_after", map[string]any{"method": "sendMessage", "error_code": 429, "description": "Too Many Requests", "retry_after": 3, "times": 1},
			2, []int{429, 200}, true},
		{"403 twice for any method", map[string]any{"error_code": 403, "description": "Forbidden: bot was blocked by the user", "times": 2},
			3, []int{403, 403, 200}, false},
		{"other methods unaffected", map[string]any{"method": "sendVideo", "error_code": 500, "description": "boom", "times": 1},
			1, []int{200}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			f.seedGroup(t, "member")
			if status, body := f.admin(t, http.MethodPost, "/_fake/faults", tt.fault); status != http.StatusOK {
				t.Fatalf("queue fault: %d %v", status, body)
			}
			for i := 0; i < tt.calls; i++ {
				status, body := f.call(t, testToken, "sendMessage", map[string]any{"chat_id": groupID, "text": "x"})
				if status != tt.statuses[i] {
					t.Fatalf("call %d: status %d, want %d (%v)", i, status, tt.statuses[i], body)
				}
				if tt.retry && status == 429 {
					params, _ := body["parameters"].(map[string]any)
					if params["retry_after"] != float64(3) {
						t.Fatalf("retry_after = %v", params)
					}
				}
			}
		})
	}
}

func TestSetMyCommandsAndDeleteWebhook(t *testing.T) {
	f := newFixture(t)
	if status, body := f.call(t, testToken, "deleteWebhook", map[string]any{"drop_pending_updates": false}); status != 200 || body["result"] != true {
		t.Fatalf("deleteWebhook = %d %v", status, body)
	}
	if status, body := f.call(t, testToken, "setMyCommands", map[string]any{"commands": []map[string]string{{"command": "start", "description": "Start"}}}); status != 200 || body["result"] != true {
		t.Fatalf("setMyCommands = %d %v", status, body)
	}
	snap := f.st.Snapshot()
	if !strings.Contains(string(snap.Commands), `"start"`) {
		t.Fatalf("commands not stored: %s", snap.Commands)
	}
	if status, _ := f.call(t, testToken, "setMyCommands", map[string]any{}); status != 400 {
		t.Fatalf("setMyCommands without commands status = %d, want 400", status)
	}
}

func TestMultipartLimitIsEnforcedBeforeSend(t *testing.T) {
	f := newFixture(t)
	f.seedGroup(t, "member")
	ctype, body := multipartVideo(t, map[string]string{"chat_id": "-100500"}, bytes.Repeat([]byte("x"), 2<<20), nil)
	res, err := post(t, f.bot.URL+"/bot"+testToken+"/sendVideo", ctype, bytes.NewReader(body)) //nolint:bodyclose // decodeEnvelope closes it
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if n := len(f.st.Sent()); n != 0 {
		t.Fatalf("oversized video was recorded (%d records)", n)
	}
}

// post sends a request with a background context; callers close the body.
func post(t *testing.T, url, contentType string, body io.Reader) (*http.Response, error) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", contentType)
	return http.DefaultClient.Do(req)
}

// get sends a GET request with a background context; callers close the body.
func get(t *testing.T, url string) (*http.Response, error) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	return http.DefaultClient.Do(req)
}
