package bot

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/tuskar2321/quoker/internal/core/accounts"
	"github.com/tuskar2321/quoker/internal/core/admin"
	"github.com/tuskar2321/quoker/internal/shared/errs"
)

// adminCommand runs an admin-only command from a private chat.
func (h *Handler) adminCommand(ctx context.Context, m *message, u accounts.User, cmd string, args []string) error {
	switch cmd {
	case "invite":
		role := accounts.RoleUser
		if len(args) > 0 && args[0] == "admin" {
			role = accounts.RoleAdmin
		}
		code, expires, err := h.d.Accounts.CreateInvite(ctx, u.ID, role, accounts.DefaultInviteTTL)
		if err != nil {
			return err
		}
		return h.reply(ctx, m, textInvite, code, role, expires.In(msk).Format("02.01.2006 15:04"))

	case "users":
		users, err := h.d.Accounts.ListUsers(ctx)
		if err != nil {
			return err
		}
		return h.reply(ctx, m, "%s", usersText(users))

	case "block", "unblock":
		id, err := userIDArg(args)
		if err != nil {
			return h.reply(ctx, m, textUsageBlock, cmd)
		}
		if cmd == "block" {
			err = h.d.Accounts.Block(ctx, u.ID, id)
		} else {
			err = h.d.Accounts.Unblock(ctx, id)
		}
		switch {
		case errs.Is(err, errs.Forbidden):
			return h.reply(ctx, m, textCantBlockSelf)
		case errs.Is(err, errs.NotFound):
			return h.reply(ctx, m, textNoSuchUser, args[0])
		case err != nil:
			return err
		case cmd == "block":
			return h.reply(ctx, m, textBlockedUser, id)
		}
		return h.reply(ctx, m, textUnblockedUser, id)

	case "status":
		text, err := h.d.Admin.StatusText(ctx)
		if err != nil {
			return err
		}
		return h.reply(ctx, m, "%s", text)

	case "workers":
		text, err := h.d.Admin.WorkersText(ctx)
		if err != nil {
			return err
		}
		return h.reply(ctx, m, "%s", text)

	case "allow_worker":
		if len(args) != 2 {
			return h.reply(ctx, m, textUsageAllow)
		}
		if err := h.d.Admin.AllowWorker(ctx, args[0], args[1]); err != nil {
			return h.failed(ctx, m, err)
		}
		return h.reply(ctx, m, textWorkerAllowed, args[0])

	case "revoke_worker":
		if len(args) != 1 {
			return h.reply(ctx, m, textUsageRevoke)
		}
		err := h.d.Admin.RevokeWorker(ctx, args[0])
		if errs.Is(err, errs.NotFound) {
			return h.reply(ctx, m, textNoSuchWorker, args[0])
		}
		if err != nil {
			return err
		}
		return h.reply(ctx, m, textWorkerRevoked, args[0])

	case "cookies":
		return h.cookies(ctx, m)
	}
	return h.reply(ctx, m, textUnknown)
}

func (h *Handler) cookies(ctx context.Context, m *message) error {
	switch {
	case m.Document == nil:
		return h.reply(ctx, m, textCookiesHowTo)
	case m.Document.FileSize > admin.MaxCookiesBytes:
		return h.reply(ctx, m, textCookiesTooBig)
	}
	content, err := h.d.Bot.DownloadFile(ctx, m.Document.FileID)
	if errs.Is(err, errs.WorkerUnavailable) {
		return h.reply(ctx, m, textCookiesNoWorker)
	}
	if err != nil {
		return err
	}
	if err := h.d.Admin.SetCookies(ctx, string(content)); err != nil {
		if errs.Is(err, errs.InvalidRequest) {
			return h.reply(ctx, m, textCookiesBad, detail(err))
		}
		return err
	}
	return h.reply(ctx, m, textCookiesSaved)
}

func (h *Handler) failed(ctx context.Context, m *message, err error) error {
	if errs.Is(err, errs.InvalidRequest) || errs.Is(err, errs.NotFound) {
		return h.reply(ctx, m, textFailed, detail(err))
	}
	return err
}

// detail is the English diagnostic of a domain error, good enough for admins.
func detail(err error) string {
	var e *errs.Error
	if ok := asError(err, &e); ok && e.Detail != "" {
		return e.Detail
	}
	return err.Error()
}

func userIDArg(args []string) (int64, error) {
	if len(args) != 1 {
		return 0, fmt.Errorf("want one argument")
	}
	return strconv.ParseInt(strings.TrimPrefix(args[0], "#"), 10, 64)
}

func usersText(users []accounts.User) string {
	if len(users) == 0 {
		return textNoUsers
	}
	var b strings.Builder
	for _, u := range users {
		name := "без Telegram"
		if u.TelegramLinked() {
			name = mention(u.TelegramUsername, u.TelegramFirstName)
		}
		fmt.Fprintf(&b, "#%d %s", u.ID, name)
		if u.Role == accounts.RoleAdmin {
			b.WriteString(", админ")
		}
		if u.Status == accounts.StatusBlocked {
			b.WriteString(", заблокирован")
		}
		if !u.LastSeenAt.IsZero() {
			fmt.Fprintf(&b, ", был %s", u.LastSeenAt.In(msk).Format("02.01 15:04"))
		}
		b.WriteString("\n")
	}
	return strings.TrimSuffix(b.String(), "\n")
}
