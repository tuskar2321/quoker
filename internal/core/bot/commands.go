package bot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/tuskar2321/quoker/internal/core/accounts"
	"github.com/tuskar2321/quoker/internal/core/settings"
	"github.com/tuskar2321/quoker/internal/shared/errs"
)

// msk is Moscow time for dates shown to admins (no DST since 2014).
var msk = time.FixedZone("MSK", 3*60*60)

const initRetry = time.Minute

type botCommand struct {
	Command     string `json:"command"`
	Description string `json:"description"`
}

var (
	privateCommands = []botCommand{{"start", "Начать"}, {"help", "Помощь"}}
	groupCommands   = []botCommand{{"connect", "Подключить группу к своему Quoker"}}
	adminCommands   = append(append([]botCommand(nil), privateCommands...),
		botCommand{"invite", "Инвайт на 7 дней (admin — с ролью администратора)"},
		botCommand{"users", "Пользователи"},
		botCommand{"block", "Заблокировать: /block <id>"},
		botCommand{"unblock", "Разблокировать: /unblock <id>"},
		botCommand{"status", "Состояние системы"},
		botCommand{"cookies", "Обновить cookies Instagram (файлом)"},
		botCommand{"workers", "Воркеры"},
		botCommand{"allow_worker", "Разрешить воркер: <id> <fingerprint>"},
		botCommand{"revoke_worker", "Отозвать воркер: <id>"},
	)
)

// Start learns the bot's username with getMe and registers its commands,
// retrying until a worker with a bot token answers. Run it in a goroutine.
func (h *Handler) Start(ctx context.Context) {
	for {
		err := h.init(ctx)
		if err == nil {
			return
		}
		h.d.Log.Info("bot not initialised yet", "err", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(initRetry):
		}
	}
}

func (h *Handler) init(ctx context.Context) error {
	if _, ok, err := h.d.Settings.Get(ctx, settings.BotToken); err != nil || !ok {
		return errors.Join(err, errors.New("bot_token is not set"))
	}
	raw, err := h.d.Bot.Call(ctx, "getMe", struct{}{})
	if err != nil {
		return err
	}
	var me struct {
		Username string `json:"username"`
	}
	if err := json.Unmarshal(raw, &me); err != nil || me.Username == "" {
		return errs.New(errs.UpstreamError, "getMe returned no username")
	}
	if err := h.d.Settings.Set(ctx, settings.BotUsername, me.Username); err != nil {
		return err
	}
	h.d.Log.Info("bot ready", "username", me.Username)
	return h.SyncCommands(ctx)
}

// SyncCommands registers the command menus: /start and /help in private
// chats, /connect in groups, and the full list in each admin's chat.
func (h *Handler) SyncCommands(ctx context.Context) error {
	if err := h.setCommands(ctx, privateCommands, map[string]any{"type": "all_private_chats"}, "private"); err != nil {
		return err
	}
	if err := h.setCommands(ctx, groupCommands, map[string]any{"type": "all_group_chats"}, "groups"); err != nil {
		return err
	}
	users, err := h.d.Accounts.ListUsers(ctx)
	if err != nil {
		return err
	}
	for _, u := range users {
		if u.Role == accounts.RoleAdmin && u.TelegramLinked() {
			if err := h.syncAdminCommands(ctx, u.TelegramUserID); err != nil {
				return err
			}
		}
	}
	return nil
}

func (h *Handler) syncAdminCommands(ctx context.Context, chatID int64) error {
	return h.setCommands(ctx, adminCommands, map[string]any{"type": "chat", "chat_id": chatID},
		"chat:"+strconv.FormatInt(chatID, 10))
}

func (h *Handler) setCommands(ctx context.Context, cmds []botCommand, scope map[string]any, key string) error {
	_, err := h.d.Bot.Enqueue(ctx, "setMyCommands", map[string]any{"commands": cmds, "scope": scope}, "commands:"+key)
	if err != nil {
		return fmt.Errorf("set commands for %s: %w", key, err)
	}
	return nil
}

func asError(err error, target **errs.Error) bool { return errors.As(err, target) }
