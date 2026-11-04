// Package botapi lets the core call the Telegram Bot API. The core never
// reaches Telegram itself: every call is a bot_call task run by a worker
// (docs/02-architecture.md), optionally awaited.
package botapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"google.golang.org/protobuf/proto"

	linkv1 "github.com/tuskar2321/quoker/gen/quoker/link/v1"
	"github.com/tuskar2321/quoker/internal/core/queue"
	"github.com/tuskar2321/quoker/internal/shared/errs"
)

const (
	// DefaultWait bounds Call when the context has no deadline.
	DefaultWait  = 20 * time.Second
	pollInterval = 200 * time.Millisecond
)

// Caller queues bot_call tasks.
type Caller struct {
	q    *queue.Store
	poll time.Duration
}

// New returns a Caller.
func New(q *queue.Store) *Caller { return &Caller{q: q, poll: pollInterval} }

// Error is a failed call; Description carries the worker's message, which
// for Bot API errors is "telegram <code>: <description>".
type Error struct {
	Code        errs.Code
	Description string
}

func (e *Error) Error() string { return fmt.Sprintf("bot call %s: %s", e.Code, e.Description) }

// Mentions reports whether the Telegram description contains s (case-insensitive).
func (e *Error) Mentions(s string) bool {
	return strings.Contains(strings.ToLower(e.Description), strings.ToLower(s))
}

// Enqueue queues method without waiting. dedup, when set, collapses
// identical pending calls (e.g. repeated setMyCommands).
func (c *Caller) Enqueue(ctx context.Context, method string, params any, dedup string) (string, error) {
	raw, err := json.Marshal(params)
	if err != nil {
		return "", fmt.Errorf("encode %s params: %w", method, err)
	}
	return c.enqueue(ctx, &linkv1.BotCallTask{Method: method, ParamsJson: string(raw)}, dedup)
}

// Call runs method and returns its "result".
func (c *Caller) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	raw, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("encode %s params: %w", method, err)
	}
	res, err := c.await(ctx, &linkv1.BotCallTask{Method: method, ParamsJson: string(raw)})
	if err != nil {
		return nil, err
	}
	return json.RawMessage(res.ResponseJson), nil
}

// DownloadFile fetches a file the bot received: getFile, then the download
// itself, both on a worker.
func (c *Caller) DownloadFile(ctx context.Context, fileID string) ([]byte, error) {
	raw, err := c.Call(ctx, "getFile", map[string]string{"file_id": fileID})
	if err != nil {
		return nil, err
	}
	var f struct {
		FilePath string `json:"file_path"`
	}
	if err := json.Unmarshal(raw, &f); err != nil || f.FilePath == "" {
		return nil, errs.New(errs.UpstreamError, "getFile returned no file_path")
	}
	res, err := c.await(ctx, &linkv1.BotCallTask{Method: "getFile", DownloadFilePath: f.FilePath})
	if err != nil {
		return nil, err
	}
	return res.FileContent, nil
}

// Message is an outgoing text message.
type Message struct {
	ChatID  int64
	Text    string
	ReplyTo int64 // message id to reply to, 0 for none
}

// Send queues a plain-text message.
func (c *Caller) Send(ctx context.Context, m Message) error {
	p := map[string]any{
		"chat_id":              m.ChatID,
		"text":                 m.Text,
		"link_preview_options": map[string]bool{"is_disabled": true},
	}
	if m.ReplyTo != 0 {
		p["reply_parameters"] = map[string]any{"message_id": m.ReplyTo, "allow_sending_without_reply": true}
	}
	_, err := c.Enqueue(ctx, "sendMessage", p, "")
	return err
}

func (c *Caller) enqueue(ctx context.Context, t *linkv1.BotCallTask, dedup string) (string, error) {
	payload, err := proto.Marshal(t)
	if err != nil {
		return "", fmt.Errorf("encode bot_call: %w", err)
	}
	id, err := c.q.Enqueue(ctx, queue.EnqueueParams{Kind: queue.KindBotCall, Payload: payload, DedupKey: dedup})
	if err != nil && !errors.Is(err, queue.ErrDuplicate) {
		return "", fmt.Errorf("enqueue bot_call: %w", err)
	}
	return id, nil
}

func (c *Caller) await(ctx context.Context, t *linkv1.BotCallTask) (*linkv1.BotCallResult, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, DefaultWait)
		defer cancel()
	}
	id, err := c.enqueue(ctx, t, "")
	if err != nil {
		return nil, err
	}
	tick := time.NewTicker(c.poll)
	defer tick.Stop()
	for {
		task, err := c.q.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		switch task.Status {
		case queue.StatusDone:
			var res linkv1.BotCallResult
			if err := proto.Unmarshal(task.Result, &res); err != nil {
				return nil, fmt.Errorf("decode bot_call result: %w", err)
			}
			return &res, nil
		case queue.StatusFailed, queue.StatusCancelled:
			return nil, &Error{Code: errs.Code(task.ErrorCode), Description: task.ErrorMsg}
		}
		select {
		case <-ctx.Done():
			return nil, errs.New(errs.WorkerUnavailable, "no worker answered "+t.Method)
		case <-tick.C:
		}
	}
}
