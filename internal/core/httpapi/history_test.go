package httpapi

import (
	"context"
	"net/http"
	"testing"

	"google.golang.org/protobuf/proto"

	linkv1 "github.com/tuskar2321/quoker/gen/quoker/link/v1"
	"github.com/tuskar2321/quoker/internal/core/accounts"
	"github.com/tuskar2321/quoker/internal/core/queue"
)

func TestHistory(t *testing.T) {
	e := newEnv(t)
	tok, uid := e.user(accounts.RoleUser)
	ctx := context.Background()

	// The user gets a video by polling its resolve task.
	res := &linkv1.ResolveResult{Platform: "tiktok", NativeId: "42", CanonicalUrl: "https://www.tiktok.com/@a/video/42", Author: "a", DurationS: 12}
	if err := e.videos.ApplyResolveResult(ctx, "", res, ""); err != nil {
		t.Fatal(err)
	}
	id, _ := e.queue.Enqueue(ctx, queue.EnqueueParams{Kind: queue.KindResolve, Payload: []byte{}})
	_, _ = e.queue.Lease(ctx, "w")
	blob, _ := proto.Marshal(res)
	if err := e.queue.Complete(ctx, queue.CompleteParams{TaskID: id, WorkerID: "w", Result: blob}); err != nil {
		t.Fatal(err)
	}
	if r := e.do(req{method: http.MethodGet, path: "/v1/tasks/" + id, token: tok}); r.body["status"] != "done" {
		t.Fatalf("task = %s", r.raw)
	}
	e.addVideo("tiktok:1", "tiktok")
	if _, err := e.st.DB().ExecContext(ctx,
		`INSERT INTO history (user_id, video_id, action, target_title, created_at) VALUES (?, 'tiktok:1', 'sent', 'Друзья', ?)`,
		uid, e.now.Add(1).UnixMilli()); err != nil {
		t.Fatal(err)
	}

	first := e.do(req{method: http.MethodGet, path: "/v1/history?limit=1", token: tok})
	items, _ := first.body["items"].([]any)
	if first.status != http.StatusOK || len(items) != 1 || first.body["next_cursor"] == nil {
		t.Fatalf("page 1 = %s", first.raw)
	}
	item := items[0].(map[string]any)
	actions := item["actions"].([]any)
	act := actions[0].(map[string]any)
	if item["video"].(map[string]any)["id"] != "tiktok:1" || act["type"] != "sent" || act["target_title"] != "Друзья" || act["at"] == nil {
		t.Fatalf("item = %v", item)
	}

	second := e.do(req{method: http.MethodGet, path: "/v1/history?limit=1&cursor=" + first.body["next_cursor"].(string), token: tok})
	items, _ = second.body["items"].([]any)
	if len(items) != 1 || second.body["next_cursor"] != nil {
		t.Fatalf("page 2 = %s", second.raw)
	}
	item = items[0].(map[string]any)
	if item["video"].(map[string]any)["id"] != "tiktok:42" || item["actions"].([]any)[0].(map[string]any)["type"] != "resolved" {
		t.Fatalf("resolved item = %v", item)
	}

	for _, bad := range []string{"limit=0", "limit=51", "cursor=@@"} {
		if r := e.do(req{method: http.MethodGet, path: "/v1/history?" + bad, token: tok}); r.errCode() != "invalid_request" {
			t.Fatalf("%s: %d %s", bad, r.status, r.raw)
		}
	}
}
