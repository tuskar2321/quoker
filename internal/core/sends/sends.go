// Package sends manages video-send records and enqueues send_video tasks.
// See docs/03-public-api.md (POST /v1/sends) and docs/06-telegram-bot.md (caption).
package sends

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"html"
	"strings"
	"time"
	"unicode/utf8"

	"google.golang.org/protobuf/proto"

	linkv1 "github.com/tuskar2321/quoker/gen/quoker/link/v1"
	"github.com/tuskar2321/quoker/internal/core/queue"
	"github.com/tuskar2321/quoker/internal/core/store"
	"github.com/tuskar2321/quoker/internal/core/targets"
	"github.com/tuskar2321/quoker/internal/core/videos"
	"github.com/tuskar2321/quoker/internal/shared/errs"
)

const (
	// CaptionMaxRunes is the Telegram caption limit in characters.
	CaptionMaxRunes = 1024
	// MaxSendBytes is the cloud Bot API upload limit (docs/11-nfr-security.md).
	MaxSendBytes  = 50 << 20
	signaturePref = "\nот "
)

// Status mirrors the DB check constraint.
type Status string

// Send statuses.
const (
	StatusQueued  Status = "queued"
	StatusSending Status = "sending"
	StatusSent    Status = "sent"
	StatusFailed  Status = "failed"
)

// Send is a row from the sends table.
type Send struct {
	ID             int64
	UserID         int64
	VideoID        string
	TargetID       int64
	IdempotencyKey string
	CaptionHTML    string
	Status         Status // sending is derived from the task lease, not stored
	TaskID         string
	TgMessageID    int64
	ErrorCode      string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// CreateParams are the input for Service.Create.
type CreateParams struct {
	VideoID           string
	TargetID          int64
	Caption           *string // nil → canonical_url; "" → no caption
	SignAsUser        bool
	IdempotencyKey    string
	CallerID          int64
	TelegramUserID    int64 // for the rights check
	TelegramUsername  string
	TelegramFirstName string
}

// Service manages sends.
type Service struct {
	st      *store.Store
	q       *queue.Store
	videos  *videos.Service
	targets *targets.Service
	now     func() time.Time
}

// New returns a Service.
func New(st *store.Store, q *queue.Store, v *videos.Service, t *targets.Service, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{st: st, q: q, videos: v, targets: t, now: now}
}

var errDuplicateKey = errors.New("duplicate idempotency key")

// Create records a send and enqueues its send_video task atomically. A repeated
// idempotency key returns the existing send unchanged.
func (s *Service) Create(ctx context.Context, p CreateParams) (*Send, error) {
	if existing, err := s.getByIdempotencyKey(ctx, p.CallerID, p.IdempotencyKey); err == nil {
		return existing, nil
	} else if !errs.Is(err, errs.NotFound) {
		return nil, err
	}

	vid, err := s.videos.Get(ctx, p.VideoID)
	if err != nil {
		return nil, err
	}
	tgt, err := s.targets.Get(ctx, p.CallerID, p.TargetID)
	if err != nil {
		return nil, err
	}
	if tgt.Status != targets.StatusActive {
		return nil, errs.New(errs.TargetInactive, "target is inactive")
	}
	if err := s.targets.CheckRights(ctx, tgt, p.TelegramUserID); err != nil {
		return nil, err
	}

	captionHTML := BuildCaption(p.Caption, vid, tgt, p.SignAsUser, p.TelegramUsername, p.TelegramFirstName)
	payload, err := proto.Marshal(&linkv1.SendVideoTask{
		VideoId:     vid.ID,
		Url:         vid.CanonicalURL,
		ChatId:      tgt.ChatID,
		CaptionHtml: captionHTML,
		MaxBytes:    MaxSendBytes,
		DurationS:   int32(vid.DurationS), //nolint:gosec // duration ≤ 600 s
		Width:       int32(vid.Width),     //nolint:gosec // pixels
		Height:      int32(vid.Height),    //nolint:gosec // pixels
	})
	if err != nil {
		return nil, fmt.Errorf("marshal send_video payload: %w", err)
	}

	now := s.now().UnixMilli()
	var sendID int64
	err = s.st.Tx(ctx, func(tx *sql.Tx) error {
		taskID, err := s.q.EnqueueTx(ctx, tx, queue.EnqueueParams{
			Kind:    queue.KindSendVideo,
			Payload: payload,
			UserID:  p.CallerID,
		})
		if err != nil {
			return fmt.Errorf("enqueue send_video: %w", err)
		}
		res, err := tx.ExecContext(ctx,
			`INSERT INTO sends (user_id, video_id, target_id, idempotency_key, caption_html,
			                    status, task_id, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, 'queued', ?, ?, ?)`,
			p.CallerID, vid.ID, tgt.ID, p.IdempotencyKey, nullStr(captionHTML),
			taskID, now, now)
		if isUniqueErr(err) {
			return errDuplicateKey // a concurrent request won; roll back our task
		}
		if err != nil {
			return fmt.Errorf("insert send: %w", err)
		}
		sendID, err = res.LastInsertId()
		return err
	})
	if errors.Is(err, errDuplicateKey) {
		return s.getByIdempotencyKey(ctx, p.CallerID, p.IdempotencyKey)
	}
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, p.CallerID, sendID)
}

const selectSend = `
	SELECT s.id, s.user_id, s.video_id, s.target_id, s.idempotency_key, s.caption_html,
	       CASE WHEN s.status = 'queued' AND t.status = 'leased' THEN 'sending' ELSE s.status END,
	       s.task_id, s.tg_message_id, s.error_code, s.created_at, s.updated_at
	FROM sends s LEFT JOIN tasks t ON t.id = s.task_id`

// Get returns a send by id, verifying ownership.
func (s *Service) Get(ctx context.Context, userID, sendID int64) (*Send, error) {
	return scanSend(s.st.DB().QueryRowContext(ctx,
		selectSend+` WHERE s.id = ? AND s.user_id = ?`, sendID, userID))
}

func (s *Service) getByIdempotencyKey(ctx context.Context, userID int64, key string) (*Send, error) {
	return scanSend(s.st.DB().QueryRowContext(ctx,
		selectSend+` WHERE s.user_id = ? AND s.idempotency_key = ?`, userID, key))
}

// MarkSent records a confirmed Telegram delivery for the send owning taskID,
// bumps the target's last_used_at and writes history. Repeats are no-ops.
func (s *Service) MarkSent(ctx context.Context, taskID string, messageID int64) error {
	return s.finish(ctx, taskID, StatusSent, messageID, "")
}

// MarkFailed records a final failure for the send owning taskID.
func (s *Service) MarkFailed(ctx context.Context, taskID, code string) error {
	if code == "" {
		code = string(errs.Internal)
	}
	return s.finish(ctx, taskID, StatusFailed, 0, code)
}

func (s *Service) finish(ctx context.Context, taskID string, st Status, messageID int64, code string) error {
	now := s.now().UnixMilli()
	return s.st.Tx(ctx, func(tx *sql.Tx) error {
		var (
			id, userID, targetID int64
			videoID, status      string
			title                sql.NullString
		)
		err := tx.QueryRowContext(ctx,
			`SELECT s.id, s.user_id, s.video_id, s.target_id, s.status, g.title
			 FROM sends s LEFT JOIN targets g ON g.id = s.target_id
			 WHERE s.task_id = ?`, taskID).
			Scan(&id, &userID, &videoID, &targetID, &status, &title)
		if errors.Is(err, sql.ErrNoRows) {
			return errs.New(errs.NotFound, "no send for task "+taskID)
		}
		if err != nil {
			return fmt.Errorf("find send: %w", err)
		}
		if Status(status) == StatusSent || Status(status) == StatusFailed {
			return nil
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE sends SET status = ?, tg_message_id = ?, error_code = ?, updated_at = ? WHERE id = ?`,
			string(st), nullInt(messageID), nullStr(code), now, id); err != nil {
			return fmt.Errorf("update send: %w", err)
		}
		if reason := lostChatReason(code); reason != "" {
			if _, err := tx.ExecContext(ctx,
				`UPDATE targets SET status = 'inactive', inactive_reason = ?, updated_at = ?
				 WHERE chat_id = (SELECT chat_id FROM targets WHERE id = ?)`, reason, now, targetID); err != nil {
				return fmt.Errorf("deactivate target: %w", err)
			}
		}
		action := "send_failed"
		if st == StatusSent {
			action = "sent"
			if _, err := tx.ExecContext(ctx,
				`UPDATE targets SET last_used_at = ? WHERE id = ?`, now, targetID); err != nil {
				return fmt.Errorf("touch target: %w", err)
			}
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO history (user_id, video_id, action, target_title, send_id, created_at)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			userID, videoID, action, title, id, now); err != nil {
			return fmt.Errorf("insert history: %w", err)
		}
		return nil
	})
}

// lostChatReason maps a Telegram send failure to why the bot can no longer
// post to the chat; "" when the failure says nothing about the chat.
func lostChatReason(code string) string {
	switch errs.Code(code) {
	case errs.TargetForbidden:
		return string(targets.ReasonBotNotAdmin)
	case errs.TargetInactive:
		return string(targets.ReasonBotRemoved)
	}
	return ""
}

func scanSend(row *sql.Row) (*Send, error) {
	var (
		snd         Send
		status      string
		captionHTML sql.NullString
		taskID      sql.NullString
		tgMessageID sql.NullInt64
		errorCode   sql.NullString
		createdMS   int64
		updatedMS   int64
	)
	err := row.Scan(
		&snd.ID, &snd.UserID, &snd.VideoID, &snd.TargetID, &snd.IdempotencyKey,
		&captionHTML, &status, &taskID, &tgMessageID, &errorCode,
		&createdMS, &updatedMS)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errs.New(errs.NotFound, "send not found")
	}
	if err != nil {
		return nil, fmt.Errorf("scan send: %w", err)
	}
	snd.Status = Status(status)
	snd.CaptionHTML = captionHTML.String
	snd.TaskID = taskID.String
	snd.TgMessageID = tgMessageID.Int64
	snd.ErrorCode = errorCode.String
	snd.CreatedAt = time.UnixMilli(createdMS).UTC()
	snd.UpdatedAt = time.UnixMilli(updatedMS).UTC()
	return &snd, nil
}

// BuildCaption builds caption_html per docs/06-telegram-bot.md: the user text
// (canonical_url when nil, none when empty), HTML-escaped and cut so that the
// whole caption fits 1024 characters; groups with signAsUser get
// "\nот @username" (or the first name when there is no username).
func BuildCaption(caption *string, vid *videos.Video, tgt targets.Target, signAsUser bool, username, firstName string) string {
	if caption != nil && *caption == "" {
		return ""
	}
	text := vid.CanonicalURL
	if caption != nil {
		text = *caption
	}

	sig := ""
	if tgt.Type == "group" && signAsUser {
		switch {
		case username != "":
			sig = signaturePref + "@" + username
		case firstName != "":
			sig = signaturePref + firstName
		}
	}
	// Truncate before escaping: the limit counts visible characters, and
	// cutting escaped text could split an entity like "&amp;".
	body := truncateRunes(text, CaptionMaxRunes-utf8.RuneCountInString(sig))
	return html.EscapeString(body) + html.EscapeString(sig)
}

func truncateRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

func nullStr(s string) sql.NullString { return sql.NullString{String: s, Valid: s != ""} }
func nullInt(v int64) sql.NullInt64   { return sql.NullInt64{Int64: v, Valid: v != 0} }

func isUniqueErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}
