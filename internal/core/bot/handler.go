package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/tuskar2321/quoker/internal/core/accounts"
	"github.com/tuskar2321/quoker/internal/core/admin"
	"github.com/tuskar2321/quoker/internal/core/botapi"
	"github.com/tuskar2321/quoker/internal/core/settings"
	"github.com/tuskar2321/quoker/internal/core/targets"
	"github.com/tuskar2321/quoker/internal/shared/errs"
)

// Telegram objects, only the fields the bot reads.
type (
	update struct {
		UpdateID     int64              `json:"update_id"`
		Message      *message           `json:"message"`
		MyChatMember *chatMemberUpdated `json:"my_chat_member"`
	}
	message struct {
		MessageID       int64     `json:"message_id"`
		From            *tgUser   `json:"from"`
		Chat            chat      `json:"chat"`
		Text            string    `json:"text"`
		Caption         string    `json:"caption"`
		Document        *document `json:"document"`
		MigrateToChatID int64     `json:"migrate_to_chat_id"`
		NewChatTitle    string    `json:"new_chat_title"`
	}
	tgUser struct {
		ID        int64  `json:"id"`
		IsBot     bool   `json:"is_bot"`
		Username  string `json:"username"`
		FirstName string `json:"first_name"`
	}
	chat struct {
		ID    int64  `json:"id"`
		Type  string `json:"type"` // private, group, supergroup, channel
		Title string `json:"title"`
	}
	document struct {
		FileID   string `json:"file_id"`
		FileName string `json:"file_name"`
		FileSize int64  `json:"file_size"`
	}
	chatMemberUpdated struct {
		Chat          chat       `json:"chat"`
		From          tgUser     `json:"from"`
		OldChatMember memberInfo `json:"old_chat_member"`
		NewChatMember memberInfo `json:"new_chat_member"`
	}
	memberInfo struct {
		Status          string `json:"status"` // creator, administrator, member, restricted, left, kicked
		CanPostMessages bool   `json:"can_post_messages"`
	}
)

// HandlerDeps are the collaborators of Handler.
type HandlerDeps struct {
	Link     *Service
	Accounts *accounts.Service
	Targets  *targets.Service
	Settings *settings.Store
	Bot      *botapi.Caller
	Admin    *admin.Service
	Log      *slog.Logger
}

// Handler turns Telegram updates into core actions and replies.
type Handler struct{ d HandlerDeps }

// NewHandler returns a Handler; it implements link.UpdateHandler.
func NewHandler(d HandlerDeps) *Handler {
	if d.Log == nil {
		d.Log = slog.Default()
	}
	return &Handler{d: d}
}

// HandleUpdate processes one update.
func (h *Handler) HandleUpdate(ctx context.Context, raw []byte) error {
	var u update
	if err := json.Unmarshal(raw, &u); err != nil {
		return fmt.Errorf("decode update: %w", err)
	}
	switch {
	case u.MyChatMember != nil:
		return h.onMembership(ctx, u.MyChatMember)
	case u.Message == nil:
		return nil
	case u.Message.Chat.Type == "private":
		return h.onPrivate(ctx, u.Message)
	case u.Message.Chat.Type == "group" || u.Message.Chat.Type == "supergroup":
		return h.onGroup(ctx, u.Message)
	}
	return nil
}

func (h *Handler) reply(ctx context.Context, m *message, format string, args ...any) error {
	return h.d.Bot.Send(ctx, botapi.Message{ChatID: m.Chat.ID, Text: fmt.Sprintf(format, args...), ReplyTo: m.MessageID})
}

func (h *Handler) say(ctx context.Context, chatID int64, format string, args ...any) error {
	return h.d.Bot.Send(ctx, botapi.Message{ChatID: chatID, Text: fmt.Sprintf(format, args...)})
}

// command splits "/cmd@bot arg1 arg2"; ok is false for plain text or a
// command addressed to another bot.
func (h *Handler) command(ctx context.Context, text string) (cmd string, args []string, ok bool) {
	fields := strings.Fields(text)
	if len(fields) == 0 || !strings.HasPrefix(fields[0], "/") {
		return "", nil, false
	}
	cmd, at, addressed := strings.Cut(strings.TrimPrefix(fields[0], "/"), "@")
	if addressed {
		name, _, _ := h.d.Settings.Get(ctx, settings.BotUsername)
		if !strings.EqualFold(at, name) {
			return "", nil, false
		}
	}
	return strings.ToLower(cmd), fields[1:], true
}

// who returns the Quoker account linked to a Telegram user.
func (h *Handler) who(ctx context.Context, tgUserID int64) (accounts.User, bool, error) {
	u, err := h.d.Accounts.UserByTelegram(ctx, tgUserID)
	if errs.Is(err, errs.NotFound) {
		return accounts.User{}, false, nil
	}
	return u, err == nil, err
}

func (h *Handler) onPrivate(ctx context.Context, m *message) error {
	if m.From == nil || m.From.IsBot {
		return nil
	}
	text := m.Text
	if m.Document != nil {
		text = m.Caption
	}
	cmd, args, ok := h.command(ctx, text)
	if ok && cmd == "start" && len(args) > 0 && strings.HasPrefix(args[0], "link_") {
		return h.link(ctx, m, strings.TrimPrefix(args[0], "link_"))
	}

	u, linked, err := h.who(ctx, m.From.ID)
	if err != nil {
		return err
	}
	switch {
	case !linked:
		return h.reply(ctx, m, textIntro)
	case u.Status == accounts.StatusBlocked:
		return h.reply(ctx, m, textBlocked)
	case !ok, cmd == "start":
		return h.reply(ctx, m, textIntro)
	case cmd == "help":
		if u.Role == accounts.RoleAdmin {
			return h.reply(ctx, m, textHelp+textAdminHelp)
		}
		return h.reply(ctx, m, textHelp)
	case u.Role == accounts.RoleAdmin:
		return h.adminCommand(ctx, m, u, cmd, args)
	}
	return h.reply(ctx, m, textUnknown)
}

func (h *Handler) link(ctx context.Context, m *message, token string) error {
	userID, err := h.d.Link.ConsumeLinkToken(ctx, token, m.From.ID, m.From.Username, m.From.FirstName)
	switch {
	case errs.Is(err, errs.Forbidden):
		return h.reply(ctx, m, textLinkTaken)
	case errs.Is(err, errs.LinkTokenExpired):
		return h.reply(ctx, m, textLinkExpired)
	case err != nil:
		return err
	}
	if err := h.reply(ctx, m, textLinked, mention(m.From.Username, m.From.FirstName)); err != nil {
		return err
	}
	u, err := h.d.Accounts.User(ctx, userID)
	if err != nil || u.Role != accounts.RoleAdmin {
		return err
	}
	if _, ok, _ := h.d.Settings.Get(ctx, settings.AdminChatID); !ok {
		if err := h.d.Settings.Set(ctx, settings.AdminChatID, strconv.FormatInt(m.From.ID, 10)); err != nil {
			return err
		}
	}
	return h.syncAdminCommands(ctx, m.From.ID)
}

func (h *Handler) onGroup(ctx context.Context, m *message) error {
	switch {
	case m.MigrateToChatID != 0:
		return h.d.Targets.UpdateChatID(ctx, m.Chat.ID, m.MigrateToChatID)
	case m.NewChatTitle != "":
		return h.d.Targets.UpdateTitle(ctx, m.Chat.ID, m.NewChatTitle)
	}
	cmd, _, ok := h.command(ctx, m.Text)
	if !ok || cmd != "connect" || m.From == nil || m.From.IsBot {
		return nil
	}
	u, linked, err := h.who(ctx, m.From.ID)
	switch {
	case err != nil:
		return err
	case !linked:
		return h.reply(ctx, m, textGroupNeedLink)
	case u.Status == accounts.StatusBlocked:
		return h.reply(ctx, m, textBlocked)
	}
	// Writing in the group proves membership; no getChatMember needed.
	if _, err := h.d.Targets.Upsert(ctx, targets.Target{
		OwnerUserID: u.ID, ChatID: m.Chat.ID, Type: "group", Title: m.Chat.Title, Status: targets.StatusActive,
	}); err != nil {
		return err
	}
	return h.reply(ctx, m, textConnected, m.Chat.Title, mention(m.From.Username, m.From.FirstName))
}

// onMembership tracks the bot's own membership (docs/06-telegram-bot.md,
// "Цели: учёт по my_chat_member").
func (h *Handler) onMembership(ctx context.Context, cm *chatMemberUpdated) error {
	now, was := cm.NewChatMember, cm.OldChatMember.Status
	gone := now.Status == "left" || now.Status == "kicked"
	switch cm.Chat.Type {
	case "group", "supergroup":
		if gone {
			return h.d.Targets.SetStatus(ctx, cm.Chat.ID, targets.StatusInactive, targets.ReasonBotRemoved)
		}
		added := (now.Status == "member" || now.Status == "administrator") && (was == "left" || was == "kicked" || was == "")
		if !added {
			return nil
		}
		if err := h.d.Targets.ReactivateBot(ctx, cm.Chat.ID); err != nil {
			return err
		}
		u, linked, err := h.who(ctx, cm.From.ID)
		if err != nil {
			return err
		}
		if u.Status == accounts.StatusBlocked {
			return nil // say nothing in a shared chat about someone's block
		}
		if !linked {
			return h.say(ctx, cm.Chat.ID, textGroupNeedLink)
		}
		if _, err := h.d.Targets.Upsert(ctx, targets.Target{
			OwnerUserID: u.ID, ChatID: cm.Chat.ID, Type: "group", Title: cm.Chat.Title, Status: targets.StatusActive,
		}); err != nil {
			return err
		}
		return h.say(ctx, cm.Chat.ID, textGroupHello, mention(u.TelegramUsername, u.TelegramFirstName))

	case "channel":
		switch {
		case gone:
			return h.d.Targets.SetStatus(ctx, cm.Chat.ID, targets.StatusInactive, targets.ReasonBotRemoved)
		case now.Status == "administrator" && now.CanPostMessages:
			if err := h.d.Targets.ReactivateBot(ctx, cm.Chat.ID); err != nil {
				return err
			}
			u, linked, err := h.who(ctx, cm.From.ID)
			if err != nil || !linked || u.Status == accounts.StatusBlocked {
				return err
			}
			_, err = h.d.Targets.Upsert(ctx, targets.Target{
				OwnerUserID: u.ID, ChatID: cm.Chat.ID, Type: "channel", Title: cm.Chat.Title, Status: targets.StatusActive,
			})
			return err
		default: // an admin without posting rights, or demoted to member
			if err := h.d.Targets.SetStatus(ctx, cm.Chat.ID, targets.StatusInactive, targets.ReasonBotNotAdmin); err != nil {
				return err
			}
			if now.Status != "administrator" {
				return nil
			}
			if _, linked, err := h.who(ctx, cm.From.ID); err != nil || !linked {
				return err
			}
			return h.say(ctx, cm.From.ID, textChannelNoPost, cm.Chat.Title)
		}
	}
	return nil
}

// mention renders "@username", or the first name when there is none.
func mention(username, firstName string) string {
	if username != "" {
		return "@" + username
	}
	return firstName
}
