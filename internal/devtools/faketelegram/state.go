// Package faketelegram is an in-memory stand-in for the Telegram Bot API. It is
// used by `make dev` and by local end-to-end checks. It models only what
// Quoker depends on: the methods the worker and core call, long polling with
// offset confirmation, chat membership, file downloads, error responses, and
// the records of what was sent. It is not a Telegram emulator.
package faketelegram

import (
	"context"
	"encoding/json"
	"sort"
	"sync"
	"time"
)

// Member is a chat member as getChatMember reports it.
type Member struct {
	Status          string `json:"status"` // creator, administrator, member, left, kicked
	CanPostMessages bool   `json:"can_post_messages,omitempty"`
}

// Chat is a chat the bot knows about. BotStatus is the bot's own membership:
// a chat with status left or kicked rejects sends with 403.
type Chat struct {
	ID        int64            `json:"id"`
	Type      string           `json:"type"` // private, group, supergroup, channel
	Title     string           `json:"title,omitempty"`
	Username  string           `json:"username,omitempty"`
	BotStatus string           `json:"bot_status"`
	Members   map[int64]Member `json:"members,omitempty"`
}

// Sent records one message the bot delivered (sendMessage or sendVideo).
type Sent struct {
	Method    string `json:"method"`
	ChatID    int64  `json:"chat_id"`
	MessageID int64  `json:"message_id"`
	Text      string `json:"text,omitempty"` // message text, or video caption
	ParseMode string `json:"parse_mode,omitempty"`
	Bytes     int64  `json:"bytes,omitempty"` // sendVideo: size of the video part
	Thumb     bool   `json:"thumb,omitempty"`
	Duration  int64  `json:"duration,omitempty"`
	Width     int64  `json:"width,omitempty"`
	Height    int64  `json:"height,omitempty"`
}

// Fault makes the next Times calls of Method fail with the given error. An
// empty Method matches every method.
type Fault struct {
	Method      string `json:"method"`
	ErrorCode   int    `json:"error_code"`
	Description string `json:"description"`
	RetryAfter  int    `json:"retry_after,omitempty"`
	Times       int    `json:"times"`
}

// Config is the fixed identity of the fake bot.
type Config struct {
	Token          string // bot token; calls with another token get 401
	BotID          int64
	BotUsername    string
	MaxUploadBytes int64 // sendVideo larger than this gets 413
}

// State holds everything the fake knows. All access goes through its mutex.
// Long polls wait on notify, which is closed and replaced on every new update.
type State struct {
	mu  sync.Mutex
	cfg Config

	chats         map[int64]*Chat
	updates       []pendingUpdate // not yet confirmed by an offset
	nextUpdateID  int64
	nextMessageID int64
	files         map[string][]byte // file_id -> content, served by getFile
	sent          []Sent
	faults        []Fault
	commands      json.RawMessage
	getUpdates    int // getUpdates calls, for checks that polling happens
	notify        chan struct{}
}

type pendingUpdate struct {
	id   int64
	body json.RawMessage
}

// NewState returns an empty State for cfg.
func NewState(cfg Config) *State {
	if cfg.MaxUploadBytes <= 0 {
		cfg.MaxUploadBytes = 50 << 20
	}
	s := &State{cfg: cfg}
	s.reset()
	return s
}

// Reset clears chats, updates, files, records and faults. The bot identity stays.
func (s *State) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reset()
}

func (s *State) reset() {
	s.chats = make(map[int64]*Chat)
	s.updates = nil
	s.nextUpdateID = 1
	s.nextMessageID = 1
	s.files = make(map[string][]byte)
	s.sent = nil
	s.faults = nil
	s.commands = nil
	s.getUpdates = 0
	if s.notify != nil {
		close(s.notify)
	}
	s.notify = make(chan struct{})
}

// wakeLocked wakes every waiting long poll. Caller holds s.mu.
func (s *State) wakeLocked() {
	close(s.notify)
	s.notify = make(chan struct{})
}

// confirmLocked drops updates the caller has confirmed by passing offset.
func (s *State) confirmLocked(offset int64) {
	if offset <= 0 {
		return
	}
	kept := s.updates[:0]
	for _, u := range s.updates {
		if u.id >= offset {
			kept = append(kept, u)
		}
	}
	s.updates = kept
}

// addUpdateLocked stores an update object and wakes pollers. It assigns
// update_id when the object has none, and returns the id.
func (s *State) addUpdateLocked(obj map[string]any) (int64, error) {
	id, _ := obj["update_id"].(float64)
	if id == 0 {
		id = float64(s.nextUpdateID)
		obj["update_id"] = id
	}
	uid := int64(id)
	if uid >= s.nextUpdateID {
		s.nextUpdateID = uid + 1
	}
	raw, err := json.Marshal(obj)
	if err != nil {
		return 0, err
	}
	s.updates = append(s.updates, pendingUpdate{id: uid, body: raw})
	s.wakeLocked()
	return uid, nil
}

// GetUpdates implements long polling: it returns at most limit pending updates
// with id >= offset, waiting up to wait for one to arrive. It returns nil when
// ctx ends first.
func (s *State) GetUpdates(ctx context.Context, offset int64, limit int, wait time.Duration) []json.RawMessage {
	var expired <-chan time.Time
	if wait > 0 {
		t := time.NewTimer(wait)
		defer t.Stop()
		expired = t.C
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.getUpdates++
	for {
		s.confirmLocked(offset)
		if len(s.updates) > 0 || wait <= 0 {
			break
		}
		notify := s.notify
		s.mu.Unlock()
		select {
		case <-notify:
			s.mu.Lock()
			continue
		case <-expired:
			s.mu.Lock()
		case <-ctx.Done():
			s.mu.Lock()
			return nil
		}
		break
	}
	if limit <= 0 || limit > len(s.updates) {
		limit = len(s.updates)
	}
	out := make([]json.RawMessage, 0, limit)
	for _, u := range s.updates[:limit] {
		out = append(out, u.body)
	}
	return out
}

// PendingUpdates returns how many updates are waiting for confirmation.
func (s *State) PendingUpdates() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.updates)
}

// Sent returns a copy of the delivered-message records, oldest first.
func (s *State) Sent() []Sent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Sent(nil), s.sent...)
}

// SetChat creates or replaces a chat. An empty BotStatus means member.
func (s *State) SetChat(c Chat) Chat {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c.BotStatus == "" {
		c.BotStatus = "member"
	}
	if c.Members == nil {
		c.Members = map[int64]Member{}
	}
	stored := c
	s.chats[c.ID] = &stored
	return stored
}

// Chats returns the known chats sorted by id.
func (s *State) Chats() []Chat {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Chat, 0, len(s.chats))
	for _, c := range s.chats {
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// AddFault queues a fault.
func (s *State) AddFault(f Fault) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if f.Times <= 0 {
		f.Times = 1
	}
	s.faults = append(s.faults, f)
}

// ClearFaults removes all queued faults.
func (s *State) ClearFaults() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.faults = nil
}

// takeFault consumes one matching fault for method, if any.
func (s *State) takeFault(method string) (Fault, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.faults {
		f := &s.faults[i]
		if f.Method != "" && f.Method != method {
			continue
		}
		out := *f
		f.Times--
		if f.Times <= 0 {
			s.faults = append(s.faults[:i], s.faults[i+1:]...)
		}
		return out, true
	}
	return Fault{}, false
}

// PutFile stores content served by getFile and GET /file/bot<token>/<path>.
func (s *State) PutFile(fileID string, content []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.files[fileID] = append([]byte(nil), content...)
}

// fileByID returns the content stored for fileID.
func (s *State) fileByID(fileID string) ([]byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.files[fileID]
	return b, ok
}

// chatForSend returns the chat when the bot may send to it.
func (s *State) chatForSend(id int64) (*Chat, *apiError) {
	c, ok := s.chats[id]
	if !ok {
		return nil, &apiError{Code: 400, Description: "Bad Request: chat not found"}
	}
	switch c.BotStatus {
	case "left":
		return nil, &apiError{Code: 403, Description: "Forbidden: bot is not a member of the chat"}
	case "kicked":
		return nil, &apiError{Code: 403, Description: "Forbidden: bot was kicked from the chat"}
	}
	return c, nil
}

// recordLocked appends a delivered message and returns its message id.
func (s *State) recordLocked(r Sent) int64 {
	r.MessageID = s.nextMessageID
	s.nextMessageID++
	s.sent = append(s.sent, r)
	return r.MessageID
}

// setCommands stores the commands passed to setMyCommands.
func (s *State) setCommands(raw json.RawMessage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.commands = append(json.RawMessage(nil), raw...)
}

// StateSummary is the admin view of the state.
type StateSummary struct {
	Bot             map[string]any  `json:"bot"`
	Chats           []Chat          `json:"chats"`
	PendingUpdates  int             `json:"pending_updates"`
	GetUpdatesCalls int             `json:"get_updates_calls"`
	Sent            int             `json:"sent"`
	Faults          []Fault         `json:"faults"`
	Commands        json.RawMessage `json:"commands,omitempty"`
}

// Snapshot returns the admin view of the state.
func (s *State) Snapshot() StateSummary {
	chats := s.Chats()
	s.mu.Lock()
	defer s.mu.Unlock()
	return StateSummary{
		Bot: map[string]any{
			"id": s.cfg.BotID, "username": s.cfg.BotUsername, "is_bot": true,
		},
		Chats:           chats,
		PendingUpdates:  len(s.updates),
		GetUpdatesCalls: s.getUpdates,
		Sent:            len(s.sent),
		Faults:          append([]Fault(nil), s.faults...),
		Commands:        s.commands,
	}
}
