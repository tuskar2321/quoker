package faketelegram

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	maxFormValueBytes  = 1 << 20
	maxPollSeconds     = 50
	defaultPollLimit   = 100
	maxPollLimit       = 100
	maxRequestBody     = 1 << 20
	documentPathPrefix = "documents/"
)

// apiError is a Bot API error: HTTP status and error_code are the same number.
type apiError struct {
	Code        int
	Description string
	RetryAfter  int
}

// params are the request parameters. JSON strings are unquoted; other JSON
// values keep their JSON text, so arrays can be decoded later. files holds
// the byte size of every file part, keyed by form field name.
type params struct {
	values map[string]string
	files  map[string]int64
}

func (p params) str(name string) string { return p.values[name] }

func (p params) int64(name string) (int64, bool) {
	v, ok := p.values[name]
	if !ok {
		return 0, false
	}
	n, err := strconv.ParseInt(v, 10, 64)
	return n, err == nil
}

// Server serves the Bot API and the admin API over one State.
type Server struct {
	st  *State
	log *slog.Logger
}

// NewServer returns a Server for st.
func NewServer(st *State, log *slog.Logger) *Server {
	if log == nil {
		log = slog.Default()
	}
	return &Server{st: st, log: log}
}

// BotHandler serves /bot<token>/<method> and /file/bot<token>/<path>.
func (s *Server) BotHandler() http.Handler {
	return http.HandlerFunc(s.serveBot)
}

func (s *Server) serveBot(w http.ResponseWriter, r *http.Request) {
	if rest, ok := strings.CutPrefix(r.URL.Path, "/file/bot"); ok {
		s.serveFile(w, r, rest)
		return
	}
	rest, ok := strings.CutPrefix(r.URL.Path, "/bot")
	if !ok {
		writeError(w, &apiError{Code: http.StatusNotFound, Description: "Not Found"})
		return
	}
	token, method, found := strings.Cut(rest, "/")
	if !found || !s.tokenOK(token) {
		writeError(w, &apiError{Code: http.StatusUnauthorized, Description: "Unauthorized"})
		return
	}
	s.serveMethod(w, r, method)
}

func (s *Server) tokenOK(token string) bool {
	return subtle.ConstantTimeCompare([]byte(token), []byte(s.st.cfg.Token)) == 1
}

// serveFile answers GET /file/bot<token>/documents/<file_id>.
func (s *Server) serveFile(w http.ResponseWriter, r *http.Request, rest string) {
	token, path, found := strings.Cut(rest, "/")
	if !found || !s.tokenOK(token) {
		writeError(w, &apiError{Code: http.StatusUnauthorized, Description: "Unauthorized"})
		return
	}
	fileID, ok := strings.CutPrefix(path, documentPathPrefix)
	if !ok {
		writeError(w, &apiError{Code: http.StatusNotFound, Description: "Not Found"})
		return
	}
	content, found := s.st.fileByID(fileID)
	if !found {
		writeError(w, &apiError{Code: http.StatusNotFound, Description: "Not Found"})
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.Itoa(len(content)))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(content)
	}
}

func (s *Server) serveMethod(w http.ResponseWriter, r *http.Request, method string) {
	p, aerr := readParams(r, s.st.cfg.MaxUploadBytes)
	if aerr != nil {
		s.logCall(method, aerr)
		writeError(w, aerr)
		return
	}
	if f, ok := s.st.takeFault(method); ok {
		aerr := &apiError{Code: f.ErrorCode, Description: f.Description, RetryAfter: f.RetryAfter}
		s.logCall(method, aerr)
		writeError(w, aerr)
		return
	}
	fn, ok := s.methods()[method]
	if !ok {
		aerr := &apiError{Code: http.StatusNotFound, Description: "Not Found"}
		s.logCall(method, aerr)
		writeError(w, aerr)
		return
	}
	res, aerr := fn(r.Context(), p)
	s.logCall(method, aerr)
	if aerr != nil {
		writeError(w, aerr)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "result": res})
}

func (s *Server) logCall(method string, aerr *apiError) {
	// The token is never logged: it is only in the URL path, which is not logged.
	if aerr != nil {
		s.log.Info("bot api call", "method", method, "status", aerr.Code, "description", aerr.Description)
		return
	}
	s.log.Info("bot api call", "method", method, "status", http.StatusOK)
}

type handlerFunc func(ctx context.Context, p params) (any, *apiError)

// methods returns the supported Bot API methods.
func (s *Server) methods() map[string]handlerFunc {
	return map[string]handlerFunc{
		"getMe":         s.getMe,
		"getUpdates":    s.getUpdatesMethod,
		"deleteWebhook": func(context.Context, params) (any, *apiError) { return true, nil },
		"setMyCommands": s.setMyCommands,
		"getChat":       s.getChat,
		"getChatMember": s.getChatMember,
		"sendMessage":   s.sendMessage,
		"sendVideo":     s.sendVideo,
		"getFile":       s.getFile,
	}
}

func (s *Server) getMe(context.Context, params) (any, *apiError) {
	return map[string]any{
		"id":              s.st.cfg.BotID,
		"is_bot":          true,
		"first_name":      "Quoker Dev",
		"username":        s.st.cfg.BotUsername,
		"can_join_groups": true,
	}, nil
}

func (s *Server) getUpdatesMethod(ctx context.Context, p params) (any, *apiError) {
	offset, _ := p.int64("offset")
	limit := defaultPollLimit
	if n, ok := p.int64("limit"); ok && n > 0 {
		limit = int(min(n, maxPollLimit))
	}
	timeout, _ := p.int64("timeout")
	if timeout > maxPollSeconds {
		timeout = maxPollSeconds
	}
	if timeout < 0 {
		timeout = 0
	}
	updates := s.st.GetUpdates(ctx, offset, limit, time.Duration(timeout)*time.Second)
	if updates == nil {
		updates = []json.RawMessage{}
	}
	return updates, nil
}

func (s *Server) setMyCommands(_ context.Context, p params) (any, *apiError) {
	raw, ok := p.values["commands"]
	if !ok {
		return nil, &apiError{Code: http.StatusBadRequest, Description: "Bad Request: commands is required"}
	}
	s.st.setCommands(json.RawMessage(raw))
	return true, nil
}

func (s *Server) getChat(_ context.Context, p params) (any, *apiError) {
	id, ok := p.int64("chat_id")
	if !ok {
		return nil, &apiError{Code: http.StatusBadRequest, Description: "Bad Request: chat_id is invalid"}
	}
	s.st.mu.Lock()
	defer s.st.mu.Unlock()
	c, ok := s.st.chats[id]
	if !ok {
		return nil, &apiError{Code: http.StatusBadRequest, Description: "Bad Request: chat not found"}
	}
	return chatJSON(c), nil
}

func (s *Server) getChatMember(_ context.Context, p params) (any, *apiError) {
	chatID, ok := p.int64("chat_id")
	if !ok {
		return nil, &apiError{Code: http.StatusBadRequest, Description: "Bad Request: chat_id is invalid"}
	}
	userID, ok := p.int64("user_id")
	if !ok {
		return nil, &apiError{Code: http.StatusBadRequest, Description: "Bad Request: user_id is invalid"}
	}
	s.st.mu.Lock()
	defer s.st.mu.Unlock()
	c, aerr := s.st.chatForSend(chatID)
	if aerr != nil {
		return nil, aerr
	}
	m, ok := c.Members[userID]
	if !ok {
		// Telegram answers "left" for users who are not in the chat.
		m = Member{Status: "left"}
	}
	out := map[string]any{
		"user":   map[string]any{"id": userID, "is_bot": false, "first_name": fmt.Sprintf("User %d", userID)},
		"status": m.Status,
	}
	if m.Status == "administrator" {
		out["can_post_messages"] = m.CanPostMessages
	}
	return out, nil
}

func (s *Server) sendMessage(_ context.Context, p params) (any, *apiError) {
	chatID, ok := p.int64("chat_id")
	if !ok {
		return nil, &apiError{Code: http.StatusBadRequest, Description: "Bad Request: chat_id is invalid"}
	}
	text := p.str("text")
	if strings.TrimSpace(text) == "" {
		return nil, &apiError{Code: http.StatusBadRequest, Description: "Bad Request: message text is empty"}
	}
	s.st.mu.Lock()
	defer s.st.mu.Unlock()
	c, aerr := s.st.chatForSend(chatID)
	if aerr != nil {
		return nil, aerr
	}
	id := s.st.recordLocked(Sent{Method: "sendMessage", ChatID: chatID, Text: text, ParseMode: p.str("parse_mode")})
	return s.messageJSON(id, c, map[string]any{"text": text}), nil
}

func (s *Server) sendVideo(_ context.Context, p params) (any, *apiError) {
	chatID, ok := p.int64("chat_id")
	if !ok {
		return nil, &apiError{Code: http.StatusBadRequest, Description: "Bad Request: chat_id is invalid"}
	}
	size, ok := p.files["video"]
	if !ok {
		return nil, &apiError{Code: http.StatusBadRequest, Description: "Bad Request: video is required"}
	}
	rec := Sent{
		Method:    "sendVideo",
		ChatID:    chatID,
		Text:      p.str("caption"),
		ParseMode: p.str("parse_mode"),
		Bytes:     size,
		Thumb:     p.str("thumbnail") != "" || p.files["thumb"] > 0 || p.files["thumbnail"] > 0,
	}
	rec.Duration, _ = p.int64("duration")
	rec.Width, _ = p.int64("width")
	rec.Height, _ = p.int64("height")

	s.st.mu.Lock()
	defer s.st.mu.Unlock()
	c, aerr := s.st.chatForSend(chatID)
	if aerr != nil {
		return nil, aerr
	}
	id := s.st.recordLocked(rec)
	video := map[string]any{
		"file_id":        fmt.Sprintf("fake-video-%d", id),
		"file_unique_id": fmt.Sprintf("fake-video-%d", id),
		"width":          rec.Width,
		"height":         rec.Height,
		"duration":       rec.Duration,
		"file_size":      size,
	}
	return s.messageJSON(id, c, map[string]any{"video": video}), nil
}

func (s *Server) getFile(_ context.Context, p params) (any, *apiError) {
	fileID := p.str("file_id")
	content, ok := s.st.fileByID(fileID)
	if !ok || fileID == "" {
		return nil, &apiError{Code: http.StatusBadRequest, Description: "Bad Request: wrong file_id"}
	}
	return map[string]any{
		"file_id":        fileID,
		"file_unique_id": fileID,
		"file_size":      len(content),
		"file_path":      documentPathPrefix + fileID,
	}, nil
}

// messageJSON builds a Telegram Message for a delivered message. The caller
// holds s.st.mu.
func (s *Server) messageJSON(id int64, c *Chat, extra map[string]any) map[string]any {
	msg := map[string]any{
		"message_id": id,
		"date":       time.Now().Unix(),
		"chat":       chatJSON(c),
		"from": map[string]any{
			"id": s.st.cfg.BotID, "is_bot": true, "first_name": "Quoker Dev", "username": s.st.cfg.BotUsername,
		},
	}
	for k, v := range extra {
		msg[k] = v
	}
	return msg
}

func chatJSON(c *Chat) map[string]any {
	out := map[string]any{"id": c.ID, "type": c.Type}
	if c.Title != "" {
		out["title"] = c.Title
	}
	if c.Username != "" {
		out["username"] = c.Username
	}
	return out
}

// readParams decodes a request body as JSON, form or multipart. File parts are
// read and counted, never stored. A body with more file bytes than maxBytes
// is answered with 413, as Telegram does for oversized uploads.
func readParams(r *http.Request, maxBytes int64) (params, *apiError) {
	p := params{values: map[string]string{}, files: map[string]int64{}}
	// Query parameters are accepted too, as Telegram does.
	for k, v := range r.URL.Query() {
		if len(v) > 0 {
			p.values[k] = v[0]
		}
	}
	if r.Body == nil || r.Method == http.MethodGet {
		return p, nil
	}
	mediaType, mtParams, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	switch mediaType {
	case "multipart/form-data":
		return p, readMultipart(r.Body, mtParams["boundary"], maxBytes, p)
	case "application/json":
		return p, readJSON(r.Body, p)
	default:
		if err := r.ParseForm(); err != nil {
			return p, &apiError{Code: http.StatusBadRequest, Description: "Bad Request: cannot parse form"}
		}
		for k, v := range r.PostForm {
			if len(v) > 0 {
				p.values[k] = v[0]
			}
		}
		return p, nil
	}
}

func readMultipart(body io.Reader, boundary string, maxBytes int64, p params) *apiError {
	if boundary == "" {
		return &apiError{Code: http.StatusBadRequest, Description: "Bad Request: multipart boundary is missing"}
	}
	var total int64
	mr := multipart.NewReader(body, boundary)
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return &apiError{Code: http.StatusBadRequest, Description: "Bad Request: malformed multipart body"}
		}
		name := part.FormName()
		if part.FileName() != "" {
			n, err := io.Copy(io.Discard, part)
			if err != nil {
				return &apiError{Code: http.StatusBadRequest, Description: "Bad Request: malformed file part"}
			}
			total += n
			p.files[name] = n
			if total > maxBytes {
				return &apiError{Code: http.StatusRequestEntityTooLarge, Description: "Request Entity Too Large"}
			}
			continue
		}
		b, err := io.ReadAll(io.LimitReader(part, maxFormValueBytes))
		if err != nil {
			return &apiError{Code: http.StatusBadRequest, Description: "Bad Request: malformed field"}
		}
		p.values[name] = string(b)
	}
	return nil
}

func readJSON(body io.Reader, p params) *apiError {
	var m map[string]json.RawMessage
	if err := json.NewDecoder(io.LimitReader(body, maxRequestBody)).Decode(&m); err != nil && !errors.Is(err, io.EOF) {
		return &apiError{Code: http.StatusBadRequest, Description: "Bad Request: can't parse JSON"}
	}
	for k, raw := range m {
		var str string
		if err := json.Unmarshal(raw, &str); err == nil {
			p.values[k] = str
			continue
		}
		p.values[k] = string(raw)
	}
	return nil
}

func writeError(w http.ResponseWriter, aerr *apiError) {
	body := map[string]any{"ok": false, "error_code": aerr.Code, "description": aerr.Description}
	if aerr.RetryAfter > 0 {
		body["parameters"] = map[string]any{"retry_after": aerr.RetryAfter}
	}
	writeJSON(w, aerr.Code, body)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
