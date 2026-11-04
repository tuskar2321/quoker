package bot_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	linkv1 "github.com/tuskar2321/quoker/gen/quoker/link/v1"
	"github.com/tuskar2321/quoker/internal/core/accounts"
	"github.com/tuskar2321/quoker/internal/core/admin"
	"github.com/tuskar2321/quoker/internal/core/bot"
	"github.com/tuskar2321/quoker/internal/core/botapi"
	"github.com/tuskar2321/quoker/internal/core/botapi/botapitest"
	"github.com/tuskar2321/quoker/internal/core/queue"
	"github.com/tuskar2321/quoker/internal/core/settings"
	"github.com/tuskar2321/quoker/internal/core/store"
	"github.com/tuskar2321/quoker/internal/core/targets"
)

type fakeLink struct{ broadcasts, shutdowns int }

func (f *fakeLink) ConnectedWorkers() []string                     { return nil }
func (f *fakeLink) BroadcastConfig(context.Context) error          { f.broadcasts++; return nil }
func (f *fakeLink) Shutdown(context.Context, string, string) error { f.shutdowns++; return nil }

type env struct {
	t       *testing.T
	st      *store.Store
	q       *queue.Store
	set     *settings.Store
	acc     *accounts.Service
	link    *bot.Service
	targets *targets.Service
	links   *fakeLink
	h       *bot.Handler
	updates int64
}

func newEnv(t *testing.T) *env {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "q.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	e := &env{t: t, st: st, q: queue.New(st, nil), set: settings.New(st.DB(), nil), links: &fakeLink{}}
	e.acc = accounts.New(st, nil, nil)
	e.link = bot.New(st, e.set, nil)
	e.targets = targets.New(st, nil)
	caller := botapi.New(e.q)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	e.h = bot.NewHandler(bot.HandlerDeps{
		Link: e.link, Accounts: e.acc, Targets: e.targets, Settings: e.set, Bot: caller,
		Admin: admin.New(admin.Deps{Store: st, Settings: e.set, Link: e.links, Bot: caller, Log: log}),
		Log:   log,
	})
	if err := e.set.Set(context.Background(), settings.BotUsername, "QuokerBot"); err != nil {
		t.Fatal(err)
	}
	return e
}

// user inserts a Quoker account linked to Telegram id tg.
func (e *env) user(role accounts.Role, tg int64, username string) int64 {
	e.t.Helper()
	res, err := e.st.DB().ExecContext(context.Background(),
		`INSERT INTO users (role, status, tg_user_id, tg_username, tg_first_name, created_at) VALUES (?, 'active', ?, ?, 'Max', ?)`,
		string(role), tg, username, time.Now().UnixMilli())
	if err != nil {
		e.t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

func (e *env) handle(body string) {
	e.t.Helper()
	e.updates++
	raw := fmt.Sprintf(`{"update_id":%d,`, e.updates) + body + `}`
	if err := e.h.HandleUpdate(context.Background(), []byte(raw)); err != nil {
		e.t.Fatalf("HandleUpdate(%s): %v", raw, err)
	}
}

func privateMsg(from int64, username, text string) string {
	return fmt.Sprintf(`"message":{"message_id":10,"from":{"id":%d,"username":%q,"first_name":"Max"},"chat":{"id":%d,"type":"private"},"text":%q}`,
		from, username, from, text)
}

func groupMsg(chatID, from int64, text string) string {
	return fmt.Sprintf(`"message":{"message_id":11,"from":{"id":%d,"username":"max","first_name":"Max"},"chat":{"id":%d,"type":"supergroup","title":"Друзья"},"text":%q}`,
		from, chatID, text)
}

func membership(chatID int64, chatType string, from int64, old, status string, canPost bool) string {
	return fmt.Sprintf(`"my_chat_member":{"chat":{"id":%d,"type":%q,"title":"Канал"},"from":{"id":%d,"first_name":"Max"},`+
		`"old_chat_member":{"status":%q},"new_chat_member":{"status":%q,"can_post_messages":%t}}`,
		chatID, chatType, from, old, status, canPost)
}

type sent struct {
	method string
	chatID int64
	text   string
	params map[string]any
}

// outbox lists queued bot_call tasks in order.
func (e *env) outbox() []sent {
	e.t.Helper()
	rows, err := e.st.DB().QueryContext(context.Background(), `SELECT payload FROM tasks WHERE kind = 'bot_call' ORDER BY created_at, id`)
	if err != nil {
		e.t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []sent
	for rows.Next() {
		var payload []byte
		_ = rows.Scan(&payload)
		var bc linkv1.BotCallTask
		_ = proto.Unmarshal(payload, &bc)
		s := sent{method: bc.Method}
		_ = json.Unmarshal([]byte(bc.ParamsJson), &s.params)
		if v, ok := s.params["chat_id"].(float64); ok {
			s.chatID = int64(v)
		}
		s.text, _ = s.params["text"].(string)
		out = append(out, s)
	}
	return out
}

func (e *env) lastText() string {
	e.t.Helper()
	box := e.outbox()
	for i := len(box) - 1; i >= 0; i-- {
		if box[i].method == "sendMessage" {
			return box[i].text
		}
	}
	e.t.Fatal("no message sent")
	return ""
}

func TestLinkFlow(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	res, _ := e.st.DB().ExecContext(context.Background(), `INSERT INTO users (role, status, created_at) VALUES ('admin', 'active', 0)`)
	uid, _ := res.LastInsertId()
	info, err := e.link.CreateLinkToken(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	token := info.DeepLink[strings.Index(info.DeepLink, "link_")+len("link_"):]

	e.handle(privateMsg(500, "maxim", "/start link_"+token))
	if got := e.lastText(); !strings.HasPrefix(got, "Готово! Quoker привязан к @maxim.") {
		t.Fatalf("reply = %q", got)
	}
	u, _ := e.acc.User(ctx, uid)
	if u.TelegramUserID != 500 {
		t.Fatalf("not linked: %+v", u)
	}
	// The first admin becomes the alert chat and gets the admin menu.
	if chat, _, _ := e.set.Get(ctx, settings.AdminChatID); chat != "500" {
		t.Fatalf("admin_chat_id = %q", chat)
	}
	var menu bool
	for _, s := range e.outbox() {
		scope, _ := s.params["scope"].(map[string]any)
		menu = menu || (s.method == "setMyCommands" && scope["chat_id"] == float64(500))
	}
	if !menu {
		t.Fatal("admin commands not registered")
	}

	e.handle(privateMsg(500, "maxim", "/start link_"+token))
	if got := e.lastText(); !strings.HasPrefix(got, "Ссылка устарела") {
		t.Fatalf("reused token reply = %q", got)
	}
}

func TestLinkTakenAndStrangers(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.user(accounts.RoleUser, 700, "taken")
	res, _ := e.st.DB().ExecContext(context.Background(), `INSERT INTO users (role, status, created_at) VALUES ('user', 'active', 0)`)
	other, _ := res.LastInsertId()
	info, _ := e.link.CreateLinkToken(ctx, other)
	token := info.DeepLink[strings.Index(info.DeepLink, "link_")+len("link_"):]

	e.handle(privateMsg(700, "taken", "/start link_"+token))
	if got := e.lastText(); !strings.HasPrefix(got, "Этот Telegram уже привязан") {
		t.Fatalf("reply = %q", got)
	}
	e.handle(privateMsg(900, "stranger", "/start"))
	if got := e.lastText(); !strings.HasPrefix(got, "Это бот приложения Quoker") {
		t.Fatalf("stranger reply = %q", got)
	}
	e.handle(privateMsg(900, "stranger", "/invite"))
	if got := e.lastText(); !strings.HasPrefix(got, "Это бот приложения Quoker") {
		t.Fatalf("stranger /invite reply = %q", got)
	}
}

func TestGroupLifecycle(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	uid := e.user(accounts.RoleUser, 600, "max")

	e.handle(membership(-100, "supergroup", 600, "left", "member", false))
	items, _ := e.targets.List(ctx, uid, false)
	if len(items) != 1 || items[0].Type != "group" || items[0].ChatID != -100 {
		t.Fatalf("targets = %+v", items)
	}
	if got := e.lastText(); got != "Привет! @max может отправлять сюда видео из Quoker. Другим участникам — команда /connect." {
		t.Fatalf("greeting = %q", got)
	}

	e.handle(groupMsg(-100, 600, "") + `,"x":0`) // a plain message: ignored
	e.handle(`"message":{"message_id":12,"chat":{"id":-100,"type":"supergroup"},"new_chat_title":"Семья"}`)
	e.handle(`"message":{"message_id":13,"chat":{"id":-100,"type":"group"},"migrate_to_chat_id":-1001}`)
	items, _ = e.targets.List(ctx, uid, false)
	if items[0].Title != "Семья" || items[0].ChatID != -1001 {
		t.Fatalf("after rename and migration: %+v", items[0])
	}

	e.handle(membership(-1001, "supergroup", 600, "member", "kicked", false))
	items, _ = e.targets.List(ctx, uid, false)
	if items[0].Status != targets.StatusInactive || items[0].InactiveReason != targets.ReasonBotRemoved {
		t.Fatalf("after removal: %+v", items[0])
	}
	e.handle(membership(-1001, "supergroup", 600, "left", "member", false))
	items, _ = e.targets.List(ctx, uid, false)
	if items[0].Status != targets.StatusActive {
		t.Fatalf("after re-adding: %+v", items[0])
	}
}

func TestGroupAddedByStranger(t *testing.T) {
	e := newEnv(t)
	e.handle(membership(-100, "group", 999, "left", "member", false))
	if got := e.lastText(); !strings.HasPrefix(got, "Чтобы отправлять сюда видео, сначала привяжите Quoker") {
		t.Fatalf("reply = %q", got)
	}
}

func TestConnect(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	uid := e.user(accounts.RoleUser, 600, "max")

	e.handle(groupMsg(-100, 600, "/connect@OtherBot"))
	if items, _ := e.targets.List(ctx, uid, false); len(items) != 0 {
		t.Fatal("command for another bot was handled")
	}
	e.handle(groupMsg(-100, 600, "/connect@QuokerBot"))
	items, _ := e.targets.List(ctx, uid, false)
	if len(items) != 1 || items[0].Title != "Друзья" {
		t.Fatalf("targets = %+v", items)
	}
	box := e.outbox()
	last := box[len(box)-1]
	reply, _ := last.params["reply_parameters"].(map[string]any)
	if last.text != "Готово, „Друзья“ появится в Quoker у @max" || reply["message_id"] != float64(11) {
		t.Fatalf("reply = %+v", last)
	}

	e.handle(groupMsg(-100, 777, "/connect"))
	if got := e.lastText(); !strings.HasPrefix(got, "Чтобы отправлять сюда видео") {
		t.Fatalf("stranger /connect reply = %q", got)
	}
}

func TestChannel(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	uid := e.user(accounts.RoleUser, 600, "max")

	e.handle(membership(-200, "channel", 600, "left", "administrator", true))
	items, _ := e.targets.List(ctx, uid, false)
	if len(items) != 1 || items[0].Type != "channel" || items[0].Status != targets.StatusActive {
		t.Fatalf("targets = %+v", items)
	}
	if len(e.outbox()) != 0 {
		t.Fatal("the bot must not write to a channel")
	}

	e.handle(membership(-200, "channel", 600, "administrator", "administrator", false))
	items, _ = e.targets.List(ctx, uid, false)
	if items[0].InactiveReason != targets.ReasonBotNotAdmin {
		t.Fatalf("after losing post rights: %+v", items[0])
	}
	box := e.outbox()
	if len(box) != 1 || box[0].chatID != 600 || box[0].text != "В канале „Канал“ у меня нет права публиковать сообщения." {
		t.Fatalf("dm = %+v", box)
	}

	e.handle(membership(-200, "channel", 600, "administrator", "administrator", true))
	if items, _ = e.targets.List(ctx, uid, false); items[0].Status != targets.StatusActive {
		t.Fatalf("after regaining rights: %+v", items[0])
	}
}

func TestAdminCommands(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	adminID := e.user(accounts.RoleAdmin, 500, "boss")
	userID := e.user(accounts.RoleUser, 600, "max")

	e.handle(privateMsg(500, "boss", "/invite admin"))
	if got := e.lastText(); !strings.HasPrefix(got, "Инвайт: ") || !strings.Contains(got, "Роль: admin") {
		t.Fatalf("invite reply = %q", got)
	}
	e.handle(privateMsg(500, "boss", "/users"))
	if got := e.lastText(); !strings.Contains(got, fmt.Sprintf("#%d @boss, админ", adminID)) || !strings.Contains(got, "@max") {
		t.Fatalf("users reply = %q", got)
	}
	e.handle(privateMsg(500, "boss", fmt.Sprintf("/block %d", adminID)))
	if got := e.lastText(); got != "Себя заблокировать нельзя." {
		t.Fatalf("self block reply = %q", got)
	}
	e.handle(privateMsg(500, "boss", fmt.Sprintf("/block #%d", userID)))
	if u, _ := e.acc.User(ctx, userID); u.Status != accounts.StatusBlocked {
		t.Fatalf("user not blocked: %+v", u)
	}
	e.handle(privateMsg(600, "max", "/help"))
	if got := e.lastText(); got != "Доступ закрыт администратором." {
		t.Fatalf("blocked user reply = %q", got)
	}
	e.handle(privateMsg(500, "boss", "/allow_worker w-1 "+strings.Repeat("AB:", 31)+"AB"))
	if got := e.lastText(); got != "Воркер w-1 разрешён." {
		t.Fatalf("allow reply = %q", got)
	}
	e.handle(privateMsg(500, "boss", "/revoke_worker w-1"))
	if got := e.lastText(); got != "Воркер w-1 отозван." || e.links.shutdowns != 1 {
		t.Fatalf("revoke reply = %q, shutdowns = %d", got, e.links.shutdowns)
	}
	e.handle(privateMsg(500, "boss", "/help"))
	if got := e.lastText(); !strings.Contains(got, "Команды администратора") {
		t.Fatalf("admin help = %q", got)
	}

	// A regular user cannot run admin commands.
	_ = e.acc.Unblock(ctx, userID)
	e.handle(privateMsg(600, "max", "/invite"))
	if got := e.lastText(); !strings.HasPrefix(got, "Не знаю такой команды") {
		t.Fatalf("user /invite reply = %q", got)
	}
}

func TestCookiesUpload(t *testing.T) {
	e := newEnv(t)
	e.user(accounts.RoleAdmin, 500, "boss")
	cookies := "# Netscape HTTP Cookie File\n.instagram.com\tTRUE\t/\tTRUE\t0\tsessionid\tabc"
	w := &botapitest.Worker{Answer: func(c botapitest.Call) botapitest.Reply {
		switch {
		case c.Method == "getFile" && c.Download != "":
			return botapitest.Reply{Content: []byte(cookies)}
		case c.Method == "getFile":
			return botapitest.Reply{Result: map[string]string{"file_path": "documents/cookies.txt"}}
		}
		return botapitest.Reply{Result: true}
	}}
	w.Start(t, e.q)

	e.handle(`"message":{"message_id":20,"from":{"id":500,"first_name":"B"},"chat":{"id":500,"type":"private"},` +
		`"caption":"/cookies","document":{"file_id":"F1","file_name":"cookies.txt","file_size":120}}`)
	stored, _, _ := e.set.Get(context.Background(), settings.InstagramCookies)
	if stored != cookies || e.links.broadcasts != 1 {
		t.Fatalf("cookies = %q, broadcasts = %d", stored, e.links.broadcasts)
	}
	if got := e.lastText(); got != "Cookies Instagram обновлены и отправлены воркерам." {
		t.Fatalf("reply = %q", got)
	}
}

// TestGroupAddedByBlockedUser: nothing is said in the shared chat and no
// target appears (REVIEW L15).
func TestGroupAddedByBlockedUser(t *testing.T) {
	e := newEnv(t)
	uid := e.user(accounts.RoleUser, 600, "max")
	if _, err := e.st.DB().ExecContext(context.Background(), `UPDATE users SET status = 'blocked' WHERE id = ?`, uid); err != nil {
		t.Fatal(err)
	}
	e.handle(membership(-100, "supergroup", 600, "left", "member", false))
	if box := e.outbox(); len(box) != 0 {
		t.Fatalf("messages = %+v", box)
	}
	if items, _ := e.targets.List(context.Background(), uid, true); len(items) != 0 {
		t.Fatalf("targets = %+v", items)
	}
}
