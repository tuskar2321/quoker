// Package updates runs the getUpdates long-poll on the worker the core picked
// as poller, forwarding every update and advancing the offset only after the
// core acknowledged it (docs/04-core-worker-protocol.md, "Апдейты бота").
package updates

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	linkv1 "github.com/tuskar2321/quoker/gen/quoker/link/v1"
	"github.com/tuskar2321/quoker/internal/worker/telegram"
)

const (
	pollTimeoutS = 50
	idleWait     = 2 * time.Second
	retryWait    = 5 * time.Second
	conflictWait = 30 * time.Second
	ackWait      = 30 * time.Second
)

// Source is the link client as seen by the poller.
type Source interface {
	CoreConfig() *linkv1.Config
	Connected() bool
	SendBotUpdate(updateID int64, updateJSON []byte) error
}

// Poller forwards Telegram updates to the core.
type Poller struct {
	tg  *telegram.Client
	src Source
	log *slog.Logger

	pollTimeoutS int
	idle, retry  time.Duration
	conflict     time.Duration
	ackWait      time.Duration

	mu    sync.Mutex
	acked int64
	ackCh chan struct{}
}

// New returns a Poller.
func New(tg *telegram.Client, src Source, log *slog.Logger) *Poller {
	if log == nil {
		log = slog.Default()
	}
	return &Poller{
		tg: tg, src: src, log: log,
		pollTimeoutS: pollTimeoutS, idle: idleWait, retry: retryWait, conflict: conflictWait, ackWait: ackWait,
		ackCh: make(chan struct{}, 1),
	}
}

// WithTimings shortens waits (for tests).
func (p *Poller) WithTimings(pollS int, idle, ack time.Duration) *Poller {
	p.pollTimeoutS, p.idle, p.retry, p.conflict, p.ackWait = pollS, idle, idle, idle, ack
	return p
}

// Ack records that the core processed updates up to id. Wire it to the link
// client's OnBotUpdateAck.
func (p *Poller) Ack(id int64) {
	p.mu.Lock()
	if id > p.acked {
		p.acked = id
	}
	p.mu.Unlock()
	select {
	case p.ackCh <- struct{}{}:
	default:
	}
}

// Run polls while this worker is the poller and connected, until ctx ends.
func (p *Poller) Run(ctx context.Context) {
	var offset int64
	for ctx.Err() == nil {
		cfg := p.src.CoreConfig()
		if cfg == nil || !cfg.PollUpdates || cfg.BotToken == "" || !p.src.Connected() {
			sleep(ctx, p.idle)
			continue
		}
		pollCtx, cancel := context.WithTimeout(ctx, time.Duration(p.pollTimeoutS+15)*time.Second)
		ups, err := p.tg.GetUpdates(pollCtx, cfg.BotApiBase, cfg.BotToken, offset, p.pollTimeoutS)
		cancel()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			var te *telegram.Error
			if errors.As(err, &te) && te.Status == http.StatusConflict {
				if strings.Contains(strings.ToLower(te.Description), "webhook") {
					// The bot was set up with a webhook elsewhere: Quoker owns
					// the bot and polls, so the webhook goes.
					p.log.Warn("bot has a webhook, deleting it to poll", "err", err)
					if _, derr := p.tg.Call(ctx, cfg.BotApiBase, cfg.BotToken, "deleteWebhook", []byte(`{"drop_pending_updates":false}`)); derr != nil {
						p.log.Error("deleteWebhook failed", "err", derr)
						sleep(ctx, p.conflict)
					}
					continue
				}
				p.log.Warn("another getUpdates consumer is active", "err", err)
				sleep(ctx, p.conflict)
				continue
			}
			p.log.Warn("getUpdates failed", "err", err)
			sleep(ctx, p.retry)
			continue
		}
		if len(ups) == 0 {
			continue
		}
		sent := true
		for _, u := range ups {
			if err := p.src.SendBotUpdate(u.ID, u.JSON); err != nil {
				p.log.Warn("forward update", "update_id", u.ID, "err", err)
				sent = false
				break
			}
		}
		last := ups[len(ups)-1].ID
		if sent && p.waitAck(ctx, last) {
			offset = last + 1
		}
	}
}

// waitAck reports whether the core acknowledged id within the ack timeout.
func (p *Poller) waitAck(ctx context.Context, id int64) bool {
	timer := time.NewTimer(p.ackWait)
	defer timer.Stop()
	for {
		p.mu.Lock()
		done := p.acked >= id
		p.mu.Unlock()
		if done {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-timer.C:
			p.log.Warn("core did not acknowledge updates, polling them again", "update_id", id)
			return false
		case <-p.ackCh:
		}
	}
}

func sleep(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}
