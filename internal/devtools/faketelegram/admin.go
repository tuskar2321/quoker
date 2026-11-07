package faketelegram

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

// AdminHandler serves the plain-HTTP control API used by `make dev` and tests:
//
//	GET    /healthz            liveness, used by compose and `faketelegram healthcheck`
//	POST   /_fake/updates      inject raw updates (object or array); update_id is assigned if 0
//	POST   /_fake/messages     inject a private or group message from a user (see messageIn)
//	POST   /_fake/chats        create or replace a chat and its members (see chatIn)
//	POST   /_fake/files        store content served by getFile (see fileIn)
//	POST   /_fake/faults       queue errors for the next calls of a method (see Fault)
//	DELETE /_fake/faults       clear queued faults
//	GET    /_fake/sent         delivered messages, oldest first
//	GET    /_fake/state        summary: chats, pending updates, getUpdates calls, faults
//	POST   /_fake/reset        clear everything except the bot identity
func (s *Server) AdminHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("POST /_fake/updates", s.postUpdates)
	mux.HandleFunc("POST /_fake/messages", s.postMessage)
	mux.HandleFunc("POST /_fake/chats", s.postChat)
	mux.HandleFunc("POST /_fake/files", s.postFile)
	mux.HandleFunc("POST /_fake/faults", s.postFault)
	mux.HandleFunc("DELETE /_fake/faults", func(w http.ResponseWriter, _ *http.Request) {
		s.st.ClearFaults()
		writeJSON(w, http.StatusOK, map[string]string{"status": "cleared"})
	})
	mux.HandleFunc("GET /_fake/sent", func(w http.ResponseWriter, _ *http.Request) {
		sent := s.st.Sent()
		if sent == nil {
			sent = []Sent{}
		}
		writeJSON(w, http.StatusOK, sent)
	})
	mux.HandleFunc("GET /_fake/state", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, s.st.Snapshot())
	})
	mux.HandleFunc("POST /_fake/reset", func(w http.ResponseWriter, _ *http.Request) {
		s.st.Reset()
		writeJSON(w, http.StatusOK, map[string]string{"status": "reset"})
	})
	return mux
}

// messageIn describes an inbound message for POST /_fake/messages.
type messageIn struct {
	ChatID    int64  `json:"chat_id"`
	ChatType  string `json:"chat_type"` // default private
	ChatTitle string `json:"chat_title"`
	UserID    int64  `json:"user_id"`
	Username  string `json:"username"`
	FirstName string `json:"first_name"`
	Text      string `json:"text"`
	// Document is a file the user sent; its content is stored with PutFile
	// through POST /_fake/files first.
	Document *struct {
		FileID   string `json:"file_id"`
		FileName string `json:"file_name"`
	} `json:"document"`
}

// chatIn describes a chat for POST /_fake/chats.
type chatIn struct {
	ID        int64  `json:"id"`
	Type      string `json:"type"`
	Title     string `json:"title"`
	Username  string `json:"username"`
	BotStatus string `json:"bot_status"` // member, administrator, left, kicked
	Members   []struct {
		UserID          int64  `json:"user_id"`
		Status          string `json:"status"`
		CanPostMessages bool   `json:"can_post_messages"`
	} `json:"members"`
}

// fileIn describes stored content for POST /_fake/files. Content is plain
// text, or base64 when ContentBase64 is set.
type fileIn struct {
	FileID        string `json:"file_id"`
	Content       string `json:"content"`
	ContentBase64 string `json:"content_base64"`
}

func (s *Server) postUpdates(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		writeAdminError(w, http.StatusBadRequest, err)
		return
	}
	var list []map[string]any
	if trimmed := bytes.TrimSpace(body); len(trimmed) > 0 && trimmed[0] == '[' {
		if err := json.Unmarshal(body, &list); err != nil {
			writeAdminError(w, http.StatusBadRequest, err)
			return
		}
	} else {
		var one map[string]any
		if err := json.Unmarshal(body, &one); err != nil {
			writeAdminError(w, http.StatusBadRequest, err)
			return
		}
		list = []map[string]any{one}
	}
	ids := make([]int64, 0, len(list))
	for _, u := range list {
		id, err := s.st.injectUpdate(u)
		if err != nil {
			writeAdminError(w, http.StatusBadRequest, err)
			return
		}
		ids = append(ids, id)
	}
	writeJSON(w, http.StatusOK, map[string]any{"update_ids": ids})
}

func (s *Server) postMessage(w http.ResponseWriter, r *http.Request) {
	var in messageIn
	if !decodeStrict(w, r, &in) {
		return
	}
	if in.UserID == 0 || in.ChatID == 0 {
		writeAdminError(w, http.StatusBadRequest, errors.New("chat_id and user_id are required"))
		return
	}
	if in.ChatType == "" {
		in.ChatType = "private"
	}
	id, err := s.st.injectMessage(in)
	if err != nil {
		writeAdminError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"update_id": id})
}

func (s *Server) postChat(w http.ResponseWriter, r *http.Request) {
	var in chatIn
	if !decodeStrict(w, r, &in) {
		return
	}
	if in.ID == 0 || in.Type == "" {
		writeAdminError(w, http.StatusBadRequest, errors.New("id and type are required"))
		return
	}
	switch in.BotStatus {
	case "", "member", "administrator", "left", "kicked":
	default:
		writeAdminError(w, http.StatusBadRequest, errors.New("bot_status must be member, administrator, left or kicked"))
		return
	}
	members := make(map[int64]Member, len(in.Members))
	for _, m := range in.Members {
		switch m.Status {
		case "creator", "administrator", "member", "restricted", "left", "kicked":
		default:
			writeAdminError(w, http.StatusBadRequest, errors.New("member status "+m.Status+" is not supported"))
			return
		}
		members[m.UserID] = Member{Status: m.Status, CanPostMessages: m.CanPostMessages}
	}
	c := s.st.SetChat(Chat{
		ID: in.ID, Type: in.Type, Title: in.Title, Username: in.Username,
		BotStatus: in.BotStatus, Members: members,
	})
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) postFile(w http.ResponseWriter, r *http.Request) {
	var in fileIn
	if !decodeStrict(w, r, &in) {
		return
	}
	if in.FileID == "" || strings.ContainsAny(in.FileID, "/ ") {
		writeAdminError(w, http.StatusBadRequest, errors.New("file_id is required and must not contain slashes or spaces"))
		return
	}
	content := []byte(in.Content)
	if in.ContentBase64 != "" {
		b, err := base64.StdEncoding.DecodeString(in.ContentBase64)
		if err != nil {
			writeAdminError(w, http.StatusBadRequest, err)
			return
		}
		content = b
	}
	if int64(len(content)) > s.st.cfg.MaxUploadBytes {
		writeAdminError(w, http.StatusRequestEntityTooLarge, errors.New("content is larger than the upload limit"))
		return
	}
	s.st.PutFile(in.FileID, content)
	writeJSON(w, http.StatusOK, map[string]any{"file_id": in.FileID, "file_size": len(content)})
}

func (s *Server) postFault(w http.ResponseWriter, r *http.Request) {
	var in Fault
	if !decodeStrict(w, r, &in) {
		return
	}
	if in.ErrorCode < 400 || in.ErrorCode > 599 {
		writeAdminError(w, http.StatusBadRequest, errors.New("error_code must be a 4xx or 5xx status"))
		return
	}
	s.st.AddFault(in)
	writeJSON(w, http.StatusOK, map[string]string{"status": "queued"})
}

// injectMessage stores a message update and registers the private chat the
// user wrote from, as Telegram does before a bot can answer.
func (s *State) injectMessage(in messageIn) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.chats[in.ChatID]; !ok {
		s.chats[in.ChatID] = &Chat{ID: in.ChatID, Type: in.ChatType, Title: in.ChatTitle, BotStatus: "member", Members: map[int64]Member{}}
	}
	chat := s.chats[in.ChatID]
	chat.Members[in.UserID] = Member{Status: "member"}
	msgID := s.nextMessageID
	s.nextMessageID++
	msg := map[string]any{
		"message_id": msgID,
		"date":       time.Now().Unix(),
		"from": map[string]any{
			"id": in.UserID, "is_bot": false, "first_name": in.FirstName, "username": in.Username,
		},
		"chat": chatJSON(chat),
	}
	if in.Text != "" {
		msg["text"] = in.Text
	}
	if in.Document != nil {
		msg["document"] = map[string]any{
			"file_id": in.Document.FileID, "file_name": in.Document.FileName, "file_unique_id": in.Document.FileID,
		}
	}
	return s.addUpdateLocked(map[string]any{"message": msg})
}

// injectUpdate stores a raw update. A my_chat_member update also moves the
// bot's own status in the chat, so later sends see the same membership.
func (s *State) injectUpdate(u map[string]any) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cm, ok := u["my_chat_member"].(map[string]any); ok {
		if err := s.applyChatMemberLocked(cm); err != nil {
			return 0, err
		}
	}
	return s.addUpdateLocked(u)
}

func (s *State) applyChatMemberLocked(cm map[string]any) error {
	chatObj, _ := cm["chat"].(map[string]any)
	newMember, _ := cm["new_chat_member"].(map[string]any)
	status, _ := newMember["status"].(string)
	user, _ := newMember["user"].(map[string]any)
	isBot, _ := user["is_bot"].(bool)
	id, _ := chatObj["id"].(float64)
	if id == 0 || status == "" || !isBot {
		return errors.New("my_chat_member needs chat.id, new_chat_member.status and an is_bot user")
	}
	c, ok := s.chats[int64(id)]
	if !ok {
		chatType, _ := chatObj["type"].(string)
		title, _ := chatObj["title"].(string)
		c = &Chat{ID: int64(id), Type: chatType, Title: title, Members: map[int64]Member{}}
		s.chats[int64(id)] = c
	}
	c.BotStatus = status
	return nil
}

// decodeStrict decodes a JSON body and rejects unknown fields, so a typo in a
// dev script fails loudly instead of being ignored.
func decodeStrict(w http.ResponseWriter, r *http.Request, v any) bool {
	body, err := readBody(r)
	if err != nil {
		writeAdminError(w, http.StatusBadRequest, err)
		return false
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeAdminError(w, http.StatusBadRequest, err)
		return false
	}
	return true
}

func readBody(r *http.Request) ([]byte, error) {
	return io.ReadAll(io.LimitReader(r.Body, maxRequestBody))
}

func writeAdminError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
