package admin

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/tuskar2321/quoker/internal/core/botapi"
	"github.com/tuskar2321/quoker/internal/core/settings"
)

const (
	monitorInterval = time.Minute
	noWorkersAfter  = 5 * time.Minute
	errorRatioPct   = 30
	errorMinTasks   = 10
	diskAlertPct    = 80
	certWarnBefore  = 30 * 24 * time.Hour
	summaryHourMSK  = 10
)

// msk is Moscow time; Russia has no DST since 2014, and a fixed zone avoids
// shipping tzdata in the image.
var msk = time.FixedZone("MSK", 3*60*60)

type alertState struct {
	noWorkersSince time.Time
	noWorkersSent  bool
	last           map[string]time.Time // alert key → last sent
	summaryDay     string
}

// Run checks the system every minute and alerts the admin chat.
func (s *Service) Run(ctx context.Context) {
	t := time.NewTicker(monitorInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := s.Check(ctx); err != nil && ctx.Err() == nil {
				s.d.Log.Warn("admin monitor", "err", err)
			}
		}
	}
}

// Check runs one round of alert rules and the daily summary.
func (s *Service) Check(ctx context.Context) error {
	now := s.d.Now()
	s.checkWorkers(ctx, now)

	hour, err := s.platformStats(ctx, now.Add(-time.Hour))
	if err != nil {
		return err
	}
	for _, p := range []string{"instagram", "tiktok"} {
		ps := hour[p]
		if ps == nil || ps.total < errorMinTasks || ps.failed*100 <= ps.total*errorRatioPct {
			continue
		}
		s.alertOnce(ctx, "errors:"+p, time.Hour, fmt.Sprintf("⚠️ %s: %d из %d запросов с ошибкой за час (%s)",
			platformName(p), ps.failed, ps.total, ps.topCode()))
	}
	if ps := hour["instagram"]; ps != nil && ps.codes["login_required"] > 0 {
		s.alertOnce(ctx, "login", time.Hour,
			"⚠️ Instagram требует вход: обновите cookies служебного аккаунта (/cookies)")
	}

	if s.d.Files != nil {
		if used, total, err := diskUsage(s.d.Files.Root()); err == nil && total > 0 {
			if pct := used * 100 / total; pct >= diskAlertPct {
				s.alertOnce(ctx, "disk", 6*time.Hour, fmt.Sprintf("⚠️ Диск ядра заполнен на %d%%", pct))
			}
		}
	}

	workers, err := s.Workers(ctx)
	if err != nil {
		return err
	}
	for _, w := range workers {
		if w.Status != "allowed" || w.CertNotAfter.IsZero() {
			continue
		}
		if left := w.CertNotAfter.Sub(now); left < certWarnBefore {
			s.alertOnce(ctx, "cert:"+w.ID, 24*time.Hour,
				fmt.Sprintf("⚠️ Сертификат %s истекает через %d дн.", w.ID, max(0, int(left.Hours()/24))))
		}
	}

	local := now.In(msk)
	day := local.Format(time.DateOnly)
	s.mu.Lock()
	due := local.Hour() == summaryHourMSK && s.alerts.summaryDay != day
	if due {
		s.alerts.summaryDay = day
	}
	s.mu.Unlock()
	if due {
		text, err := s.SummaryText(ctx)
		if err != nil {
			return err
		}
		s.notify(ctx, text)
	}
	return nil
}

func (s *Service) checkWorkers(ctx context.Context, now time.Time) {
	connected := len(s.d.Link.ConnectedWorkers()) > 0
	s.mu.Lock()
	var msg string
	switch {
	case connected && s.alerts.noWorkersSent:
		msg = "✅ Воркер снова на связи"
		s.alerts.noWorkersSent = false
		s.alerts.noWorkersSince = time.Time{}
	case connected:
		s.alerts.noWorkersSince = time.Time{}
	case s.alerts.noWorkersSince.IsZero():
		s.alerts.noWorkersSince = now
	case !s.alerts.noWorkersSent && now.Sub(s.alerts.noWorkersSince) >= noWorkersAfter:
		msg = "⚠️ Нет подключённых воркеров 5 минут"
		s.alerts.noWorkersSent = true
	}
	s.mu.Unlock()
	if msg != "" {
		s.notify(ctx, msg)
	}
}

// alertOnce sends text unless the same alert went out within every.
func (s *Service) alertOnce(ctx context.Context, key string, every time.Duration, text string) {
	now := s.d.Now()
	s.mu.Lock()
	if last, ok := s.alerts.last[key]; ok && now.Sub(last) < every {
		s.mu.Unlock()
		return
	}
	s.alerts.last[key] = now
	s.mu.Unlock()
	s.notify(ctx, text)
}

func (s *Service) notify(ctx context.Context, text string) {
	if err := s.Notify(ctx, text); err != nil {
		s.d.Log.Warn("admin alert not sent", "err", err, "alert", text)
	}
}

// Notify messages the admin chat.
func (s *Service) Notify(ctx context.Context, text string) error {
	raw, ok, err := s.d.Settings.Get(ctx, settings.AdminChatID)
	if err != nil {
		return err
	}
	chatID, perr := strconv.ParseInt(raw, 10, 64)
	if !ok || perr != nil || s.d.Bot == nil {
		return errNoAdminChat
	}
	return s.d.Bot.Send(ctx, botapi.Message{ChatID: chatID, Text: text})
}

// SummaryText renders the daily summary.
func (s *Service) SummaryText(ctx context.Context) (string, error) {
	st, err := s.Status(ctx)
	if err != nil {
		return "", err
	}
	day, err := s.platformStats(ctx, s.d.Now().Add(-24*time.Hour))
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString("📊 Сводка за сутки\n")
	for _, p := range []string{"tiktok", "instagram", "telegram"} {
		ps := day[p]
		if ps == nil {
			ps = &platformStat{}
		}
		fmt.Fprintf(&b, "%s: %d задач, ошибок %d\n", platformName(p), ps.total, ps.failed)
	}
	connected := 0
	for _, w := range st.Workers {
		if w.Connected {
			connected++
		}
	}
	fmt.Fprintf(&b, "Воркеры на связи: %d из %d\n", connected, len(st.Workers))
	for _, w := range st.Workers {
		if w.Version != "" {
			fmt.Fprintf(&b, "• %s: %s, yt-dlp %s\n", w.ID, w.Version, firstNonEmpty(w.YtdlpVersion, "?"))
		}
	}
	fmt.Fprintf(&b, "Кэш: %s из %s\n", humanBytes(st.Disk.CacheBytes), humanBytes(st.Disk.CacheMax))
	if st.Disk.TotalBytes > 0 {
		fmt.Fprintf(&b, "Диск: занято %d%%\n", st.Disk.UsedBytes*100/st.Disk.TotalBytes)
	}
	fmt.Fprintf(&b, "Активных пользователей: %d", st.ActiveUsers)
	return b.String(), nil
}

func platformName(p string) string {
	switch p {
	case "tiktok":
		return "TikTok"
	case "instagram":
		return "Instagram"
	}
	return "Telegram"
}
