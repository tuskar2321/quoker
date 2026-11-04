// Package admin provides system status, worker management, Instagram cookies,
// alerts and the daily summary for the admin bot commands and API
// (docs/06-telegram-bot.md, docs/03-public-api.md).
package admin

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"

	linkv1 "github.com/tuskar2321/quoker/gen/quoker/link/v1"
	"github.com/tuskar2321/quoker/internal/core/botapi"
	"github.com/tuskar2321/quoker/internal/core/files"
	"github.com/tuskar2321/quoker/internal/core/settings"
	"github.com/tuskar2321/quoker/internal/core/store"
	"github.com/tuskar2321/quoker/internal/shared/errs"
)

// MaxCookiesBytes is the cookies.txt limit (docs/11-nfr-security.md).
const MaxCookiesBytes = 64 << 10

// Link is the part of the worker link server admin needs.
type Link interface {
	ConnectedWorkers() []string
	BroadcastConfig(ctx context.Context) error
	Shutdown(ctx context.Context, workerID, reason string) error
}

// Deps are the collaborators of Service.
type Deps struct {
	Store    *store.Store
	Settings *settings.Store
	Link     Link
	Files    *files.Store
	Bot      *botapi.Caller
	Log      *slog.Logger
	Now      func() time.Time
}

// Service implements admin operations.
type Service struct {
	d Deps

	mu     sync.Mutex
	alerts alertState
}

// New returns a Service.
func New(d Deps) *Service {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Log == nil {
		d.Log = slog.Default()
	}
	return &Service{d: d, alerts: alertState{last: map[string]time.Time{}}}
}

// Worker is a row of the workers table plus its live state.
type Worker struct {
	ID           string
	Fingerprint  string
	Status       string
	Connected    bool
	IsPoller     bool
	Version      string
	YtdlpVersion string
	CertNotAfter time.Time
	LastSeenAt   time.Time
}

// Workers lists all known workers.
func (s *Service) Workers(ctx context.Context) ([]Worker, error) {
	rows, err := s.d.Store.DB().QueryContext(ctx,
		`SELECT id, fingerprint, status, version, ytdlp_version, cert_not_after, last_seen_at FROM workers ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list workers: %w", err)
	}
	defer func() { _ = rows.Close() }()
	connected := s.d.Link.ConnectedWorkers()
	poller, _, _ := s.d.Settings.Get(ctx, settings.PollerWorkerID)
	var out []Worker
	for rows.Next() {
		var (
			w                Worker
			version, ytdlp   sql.NullString
			notAfter, lastMS sql.NullInt64
		)
		if err := rows.Scan(&w.ID, &w.Fingerprint, &w.Status, &version, &ytdlp, &notAfter, &lastMS); err != nil {
			return nil, fmt.Errorf("scan worker: %w", err)
		}
		w.Version, w.YtdlpVersion = version.String, ytdlp.String
		w.Connected = slices.Contains(connected, w.ID)
		w.IsPoller = w.ID == poller
		if notAfter.Valid {
			w.CertNotAfter = time.UnixMilli(notAfter.Int64).UTC()
		}
		if lastMS.Valid {
			w.LastSeenAt = time.UnixMilli(lastMS.Int64).UTC()
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

var (
	workerIDPattern    = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)
	fingerprintPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// NormalizeFingerprint lowercases a SHA-256 fingerprint and drops colons.
func NormalizeFingerprint(fp string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(fp), ":", ""))
}

// AllowWorker registers or re-allows a worker certificate.
func (s *Service) AllowWorker(ctx context.Context, id, fingerprint string) error {
	fp := NormalizeFingerprint(fingerprint)
	if !workerIDPattern.MatchString(id) || !fingerprintPattern.MatchString(fp) {
		return errs.New(errs.InvalidRequest, "worker id or fingerprint is malformed")
	}
	_, err := s.d.Store.DB().ExecContext(ctx,
		`INSERT INTO workers (id, fingerprint, status, created_at) VALUES (?, ?, 'allowed', ?)
		 ON CONFLICT(id) DO UPDATE SET fingerprint = excluded.fingerprint, status = 'allowed'`,
		id, fp, s.d.Now().UnixMilli())
	if err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed: workers.fingerprint") {
		return errs.New(errs.InvalidRequest, "fingerprint belongs to another worker")
	}
	return err
}

// RevokeWorker revokes a worker and drops its stream.
func (s *Service) RevokeWorker(ctx context.Context, id string) error {
	res, err := s.d.Store.DB().ExecContext(ctx, `UPDATE workers SET status = 'revoked' WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("revoke worker: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errs.New(errs.NotFound, "no such worker")
	}
	return s.d.Link.Shutdown(ctx, id, "revoked")
}

// SetCookies stores Instagram cookies and pushes them to the workers.
func (s *Service) SetCookies(ctx context.Context, cookies string) error {
	cookies = strings.TrimSpace(cookies)
	switch {
	case cookies == "":
		return errs.New(errs.InvalidRequest, "cookies are empty")
	case len(cookies) > MaxCookiesBytes:
		return errs.New(errs.InvalidRequest, "cookies are larger than 64 KB")
	case !strings.Contains(cookies, "instagram.com"):
		return errs.New(errs.InvalidRequest, "no instagram.com cookies in the file")
	}
	if err := s.d.Settings.Set(ctx, settings.InstagramCookies, cookies); err != nil {
		return err
	}
	return s.d.Link.BroadcastConfig(ctx)
}

// Status is a system snapshot.
type Status struct {
	Workers     []Worker
	Queued      int
	Running     int
	Errors24h   map[string]int // tiktok, instagram, telegram
	Disk        Disk
	ActiveUsers int // seen in the last 24 h
}

// Disk describes core storage.
type Disk struct {
	UsedBytes  int64 // filesystem holding the cache
	TotalBytes int64
	CacheBytes int64
	CacheMax   int64
}

// Status collects the snapshot.
func (s *Service) Status(ctx context.Context) (Status, error) {
	var st Status
	var err error
	if st.Workers, err = s.Workers(ctx); err != nil {
		return st, err
	}
	db := s.d.Store.DB()
	if err := db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(status = 'queued'), 0), COALESCE(SUM(status = 'leased'), 0)
		 FROM tasks WHERE status IN ('queued', 'leased')`).Scan(&st.Queued, &st.Running); err != nil {
		return st, fmt.Errorf("queue counts: %w", err)
	}
	day := s.d.Now().Add(-24 * time.Hour)
	stats, err := s.platformStats(ctx, day)
	if err != nil {
		return st, err
	}
	st.Errors24h = map[string]int{"tiktok": 0, "instagram": 0, "telegram": 0}
	for p, ps := range stats {
		st.Errors24h[p] = ps.failed
	}
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM users WHERE last_seen_at >= ?`, day.UnixMilli()).Scan(&st.ActiveUsers); err != nil {
		return st, fmt.Errorf("active users: %w", err)
	}
	if s.d.Files != nil {
		st.Disk.CacheBytes, _ = s.d.Files.UsedBytes()
		st.Disk.CacheMax = s.d.Files.MaxBytes()
		st.Disk.UsedBytes, st.Disk.TotalBytes, _ = diskUsage(s.d.Files.Root())
	}
	return st, nil
}

type platformStat struct {
	total, failed int
	codes         map[string]int
}

// topCode returns the most frequent error code.
func (p *platformStat) topCode() string {
	var best string
	for c, n := range p.codes {
		if n > p.codes[best] || (n == p.codes[best] && c < best) {
			best = c
		}
	}
	return best
}

// platformStats counts finished tasks since t by platform: resolve and
// download by the video's host, Telegram calls as "telegram".
func (s *Service) platformStats(ctx context.Context, since time.Time) (map[string]*platformStat, error) {
	rows, err := s.d.Store.DB().QueryContext(ctx,
		`SELECT kind, payload, status, COALESCE(error_code, '') FROM tasks
		 WHERE updated_at >= ? AND status IN ('done', 'failed')`, since.UnixMilli())
	if err != nil {
		return nil, fmt.Errorf("task stats: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]*platformStat{}
	for rows.Next() {
		var kind, status, code string
		var payload []byte
		if err := rows.Scan(&kind, &payload, &status, &code); err != nil {
			return nil, fmt.Errorf("scan task stats: %w", err)
		}
		p := taskPlatform(kind, payload)
		if p == "" {
			continue
		}
		ps := out[p]
		if ps == nil {
			ps = &platformStat{codes: map[string]int{}}
			out[p] = ps
		}
		ps.total++
		if status == "failed" {
			ps.failed++
			ps.codes[code]++
		}
	}
	return out, rows.Err()
}

func taskPlatform(kind string, payload []byte) string {
	var raw string
	switch kind {
	case "send_video", "bot_call":
		return "telegram"
	case "resolve":
		var t linkv1.ResolveTask
		if proto.Unmarshal(payload, &t) == nil {
			raw = t.Url
		}
	case "download":
		var t linkv1.DownloadTask
		if proto.Unmarshal(payload, &t) == nil {
			raw = t.Url
		}
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	switch h := u.Hostname(); {
	case strings.HasSuffix(h, "tiktok.com"):
		return "tiktok"
	case strings.HasSuffix(h, "instagram.com"):
		return "instagram"
	}
	return ""
}

// StatusText renders Status for the /status command.
func (s *Service) StatusText(ctx context.Context) (string, error) {
	st, err := s.Status(ctx)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	connected := 0
	for _, w := range st.Workers {
		if w.Connected {
			connected++
		}
	}
	fmt.Fprintf(&b, "Воркеры: %d из %d на связи\n", connected, len(st.Workers))
	for _, w := range st.Workers {
		b.WriteString(workerLine(w))
	}
	fmt.Fprintf(&b, "Очередь: %d ждут, %d выполняются\n", st.Queued, st.Running)
	fmt.Fprintf(&b, "Ошибки за 24 ч: TikTok %d, Instagram %d, Telegram %d\n",
		st.Errors24h["tiktok"], st.Errors24h["instagram"], st.Errors24h["telegram"])
	fmt.Fprintf(&b, "Кэш: %s из %s\n", humanBytes(st.Disk.CacheBytes), humanBytes(st.Disk.CacheMax))
	if st.Disk.TotalBytes > 0 {
		fmt.Fprintf(&b, "Диск: занято %d%%\n", st.Disk.UsedBytes*100/st.Disk.TotalBytes)
	}
	fmt.Fprintf(&b, "Активных пользователей за 24 ч: %d", st.ActiveUsers)
	return b.String(), nil
}

// WorkersText renders the /workers command.
func (s *Service) WorkersText(ctx context.Context) (string, error) {
	ws, err := s.Workers(ctx)
	if err != nil {
		return "", err
	}
	if len(ws) == 0 {
		return "Воркеров нет. Добавьте: /allow_worker <id> <fingerprint>", nil
	}
	var b strings.Builder
	for _, w := range ws {
		b.WriteString(workerLine(w))
	}
	return strings.TrimSuffix(b.String(), "\n"), nil
}

func workerLine(w Worker) string {
	state := "не на связи"
	switch {
	case w.Status == "revoked":
		state = "отозван"
	case w.Connected:
		state = "на связи"
	}
	line := fmt.Sprintf("• %s — %s", w.ID, state)
	if w.IsPoller {
		line += ", опрашивает бота"
	}
	if w.Version != "" {
		line += fmt.Sprintf(", %s, yt-dlp %s", w.Version, firstNonEmpty(w.YtdlpVersion, "?"))
	}
	return line + "\n"
}

func humanBytes(n int64) string {
	const gb, mb = 1 << 30, 1 << 20
	if n >= gb {
		return fmt.Sprintf("%.1f ГБ", float64(n)/gb)
	}
	return fmt.Sprintf("%d МБ", n/mb)
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

var errNoAdminChat = errors.New("admin_chat_id is not set")
