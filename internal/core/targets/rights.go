package targets

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/tuskar2321/quoker/internal/shared/errs"
)

const (
	// rightsTTL is how long a successful getChatMember check is trusted.
	rightsTTL = 5 * time.Minute
	// DefaultRightsWait bounds one getChatMember call, so a missing worker
	// leaves the caller's request time to go on without the check.
	DefaultRightsWait = 5 * time.Second
)

// BotError lets rights checks tell "user not in chat" from "bot not in chat"
// without importing the caller's error type.
type BotError interface {
	error
	Mentions(s string) bool
}

type chatMember struct {
	Status          string `json:"status"`
	CanPostMessages bool   `json:"can_post_messages"`
	CanSendVideos   *bool  `json:"can_send_videos"`
	CanSendMedia    *bool  `json:"can_send_media_messages"`
}

// allowed applies docs/06-telegram-bot.md: members may post to groups,
// only admins with can_post_messages to channels.
func (m chatMember) allowed(targetType string) bool {
	switch m.Status {
	case "creator":
		return true
	case "administrator":
		return targetType != "channel" || m.CanPostMessages
	case "member":
		return targetType == "group"
	case "restricted":
		if targetType != "group" {
			return false
		}
		for _, p := range []*bool{m.CanSendVideos, m.CanSendMedia} {
			if p != nil {
				return *p
			}
		}
		return false
	}
	return false
}

// CheckRights confirms that tgUserID may still post to t, trusting a check
// younger than five minutes. A lost right deactivates the target and returns
// target_forbidden; a vanished chat returns target_inactive. When no worker
// answers, the send goes ahead and Telegram has the final word.
func (s *Service) CheckRights(ctx context.Context, t Target, tgUserID int64) error {
	if s.bot == nil || t.Type == "private" || s.now().Sub(t.RightsCheckedAt) < rightsTTL {
		return nil
	}
	ok, err := s.memberAllowed(ctx, t, tgUserID)
	switch {
	case errs.Is(err, errs.WorkerUnavailable):
		slog.WarnContext(ctx, "rights check skipped", "target_id", t.ID, "err", err)
		return nil
	case err != nil:
		return err
	case !ok:
		reason := ReasonUserLeft
		if t.Type == "channel" {
			reason = ReasonUserNotAdmin
		}
		if err := s.setStatusByID(ctx, t.ID, StatusInactive, reason); err != nil {
			return err
		}
		return errs.New(errs.TargetForbidden, "user lost rights in chat")
	}
	_, err = s.st.DB().ExecContext(ctx,
		`UPDATE targets SET rights_checked_at = ? WHERE id = ?`, s.now().UnixMilli(), t.ID)
	return err
}

// memberAllowed asks Telegram about tgUserID in t's chat. A chat the bot can
// no longer see deactivates every target of that chat.
func (s *Service) memberAllowed(ctx context.Context, t Target, tgUserID int64) (bool, error) {
	callCtx, cancel := context.WithTimeout(ctx, s.rightsWait)
	raw, err := s.bot.Call(callCtx, "getChatMember", map[string]int64{"chat_id": t.ChatID, "user_id": tgUserID})
	cancel()
	var be BotError
	switch {
	case errors.As(err, &be) && (be.Mentions("user not found") || be.Mentions("participant_id_invalid") || be.Mentions("member not found")):
		return false, nil
	case errors.As(err, &be) && (be.Mentions("chat not found") || be.Mentions("kicked") || be.Mentions("not a member")):
		if err := s.SetStatus(ctx, t.ChatID, StatusInactive, ReasonBotRemoved); err != nil {
			return false, err
		}
		return false, errs.New(errs.TargetInactive, "bot is no longer in the chat")
	case err != nil:
		return false, err
	}
	var m chatMember
	if err := json.Unmarshal(raw, &m); err != nil {
		return false, fmt.Errorf("decode chat member: %w", err)
	}
	return m.allowed(t.Type), nil
}

// Refresh re-checks every group and channel target of the user, reviving
// targets whose rights came back, and adds channels where the bot already
// posts and the user is an admin (POST /v1/targets/refresh).
func (s *Service) Refresh(ctx context.Context, userID, tgUserID int64) error {
	if s.bot == nil || tgUserID == 0 {
		return nil
	}
	own, err := s.List(ctx, userID, true)
	if err != nil {
		return err
	}
	mine := map[int64]bool{}
	for _, t := range own {
		mine[t.ChatID] = true
		if t.Type == "private" || t.InactiveReason == ReasonBotRemoved || t.InactiveReason == ReasonBotNotAdmin {
			continue
		}
		ok, err := s.memberAllowed(ctx, t, tgUserID)
		if errs.Is(err, errs.TargetInactive) {
			continue
		}
		if err != nil {
			return err
		}
		switch {
		case ok && t.Status == StatusInactive:
			err = s.setStatusByID(ctx, t.ID, StatusActive, "")
		case !ok && t.Status == StatusActive:
			reason := ReasonUserLeft
			if t.Type == "channel" {
				reason = ReasonUserNotAdmin
			}
			err = s.setStatusByID(ctx, t.ID, StatusInactive, reason)
		}
		if err != nil {
			return err
		}
	}

	channels, err := s.knownChannels(ctx)
	if err != nil {
		return err
	}
	for _, c := range channels {
		if mine[c.ChatID] {
			continue
		}
		ok, err := s.memberAllowed(ctx, c, tgUserID)
		if err != nil || !ok {
			continue
		}
		if _, err := s.Upsert(ctx, Target{OwnerUserID: userID, ChatID: c.ChatID, Type: "channel", Title: c.Title, Status: StatusActive}); err != nil {
			return err
		}
	}
	return nil
}

// knownChannels lists channels where the bot can post for someone.
func (s *Service) knownChannels(ctx context.Context) ([]Target, error) {
	rows, err := s.st.DB().QueryContext(ctx,
		`SELECT chat_id, MAX(title) FROM targets WHERE type = 'channel' AND status = 'active' GROUP BY chat_id`)
	if err != nil {
		return nil, fmt.Errorf("list channels: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Target
	for rows.Next() {
		t := Target{Type: "channel"}
		if err := rows.Scan(&t.ChatID, &t.Title); err != nil {
			return nil, fmt.Errorf("scan channel: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ReactivateBot revives targets of chatID that were off because of the bot,
// after it was added back or regained rights.
func (s *Service) ReactivateBot(ctx context.Context, chatID int64) error {
	_, err := s.st.DB().ExecContext(ctx,
		`UPDATE targets SET status = 'active', inactive_reason = NULL, updated_at = ?
		 WHERE chat_id = ? AND inactive_reason IN ('bot_removed', 'bot_not_admin')`,
		s.now().UnixMilli(), chatID)
	return err
}

func (s *Service) setStatusByID(ctx context.Context, id int64, st Status, reason InactiveReason) error {
	_, err := s.st.DB().ExecContext(ctx,
		`UPDATE targets SET status = ?, inactive_reason = ?, updated_at = ? WHERE id = ?`,
		string(st), nullableReason(reason), s.now().UnixMilli(), id)
	return err
}
