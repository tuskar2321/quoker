package faketelegram_test

import (
	"net/http"
	"testing"
)

func TestAdminUpdatesInjection(t *testing.T) {
	tests := []struct {
		name       string
		body       any
		wantStatus int
		wantIDs    int
	}{
		{"single update gets an id", map[string]any{"message": map[string]any{"text": "hi"}}, http.StatusOK, 1},
		{"array of updates", []map[string]any{{"message": map[string]any{"text": "a"}}, {"message": map[string]any{"text": "b"}}}, http.StatusOK, 2},
		{"my_chat_member for a non-bot is rejected", map[string]any{"my_chat_member": map[string]any{
			"chat":            map[string]any{"id": groupID, "type": "group"},
			"new_chat_member": map[string]any{"status": "member", "user": map[string]any{"is_bot": false}},
		}}, http.StatusBadRequest, 0},
		{"malformed JSON is rejected", "not json", http.StatusBadRequest, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			status, body := f.admin(t, http.MethodPost, "/_fake/updates", tt.body)
			if status != tt.wantStatus {
				t.Fatalf("status = %d, want %d (%v)", status, tt.wantStatus, body)
			}
			if ids, _ := body["update_ids"].([]any); len(ids) != tt.wantIDs {
				t.Fatalf("update_ids = %v, want %d", body["update_ids"], tt.wantIDs)
			}
		})
	}
}

func TestMyChatMemberMovesBotStatus(t *testing.T) {
	f := newFixture(t)
	f.seedGroup(t, "administrator")
	update := func(status string) map[string]any {
		return map[string]any{"my_chat_member": map[string]any{
			"chat":            map[string]any{"id": groupID, "type": "supergroup", "title": "Friends"},
			"new_chat_member": map[string]any{"status": status, "user": map[string]any{"is_bot": true}},
		}}
	}
	tests := []struct {
		name     string
		status   string
		wantSend int
	}{
		{"kicked blocks sends", "kicked", http.StatusForbidden},
		{"re-added restores sends", "administrator", http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if status, body := f.admin(t, http.MethodPost, "/_fake/updates", update(tt.status)); status != http.StatusOK {
				t.Fatalf("inject: %d %v", status, body)
			}
			status, body := f.call(t, testToken, "sendMessage", map[string]any{"chat_id": groupID, "text": "x"})
			if status != tt.wantSend {
				t.Fatalf("sendMessage = %d, want %d (%v)", status, tt.wantSend, body)
			}
		})
	}
}

func TestAdminMessageRegistersPrivateChat(t *testing.T) {
	f := newFixture(t)
	if status, body := f.admin(t, http.MethodPost, "/_fake/messages", map[string]any{
		"chat_id": userID, "user_id": userID, "username": "max", "first_name": "Max", "text": "/start link_abc",
	}); status != http.StatusOK {
		t.Fatalf("inject: %d %v", status, body)
	}
	if status, body := f.call(t, testToken, "sendMessage", map[string]any{"chat_id": userID, "text": "linked"}); status != http.StatusOK {
		t.Fatalf("reply to private chat: %d %v", status, body)
	}
	_, state := f.admin(t, http.MethodGet, "/_fake/state", nil)
	if n, _ := state["pending_updates"].(float64); n != 1 {
		t.Fatalf("pending_updates = %v, want 1", state["pending_updates"])
	}
}

func TestAdminMessageRequiresIDs(t *testing.T) {
	f := newFixture(t)
	if status, _ := f.admin(t, http.MethodPost, "/_fake/messages", map[string]any{"text": "no ids"}); status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", status)
	}
}

func TestAdminChatValidation(t *testing.T) {
	tests := []struct {
		name       string
		body       map[string]any
		wantStatus int
	}{
		{"valid chat", map[string]any{"id": groupID, "type": "channel", "bot_status": "administrator",
			"members": []map[string]any{{"user_id": 1, "status": "creator"}}}, http.StatusOK},
		{"unknown bot status", map[string]any{"id": groupID, "type": "group", "bot_status": "banned"}, http.StatusBadRequest},
		{"unknown member status", map[string]any{"id": groupID, "type": "group",
			"members": []map[string]any{{"user_id": 1, "status": "vip"}}}, http.StatusBadRequest},
		{"misspelled field is rejected", map[string]any{"id": groupID, "type": "group", "bot_statsu": "member"}, http.StatusBadRequest},
		{"missing type", map[string]any{"id": groupID}, http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			if status, body := f.admin(t, http.MethodPost, "/_fake/chats", tt.body); status != tt.wantStatus {
				t.Fatalf("status = %d, want %d (%v)", status, tt.wantStatus, body)
			}
		})
	}
}

func TestAdminFilesAndFaultsValidation(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		body       map[string]any
		wantStatus int
	}{
		{"file ok", "/_fake/files", map[string]any{"file_id": "abc_1", "content": "x"}, http.StatusOK},
		{"file id with slash", "/_fake/files", map[string]any{"file_id": "a/b", "content": "x"}, http.StatusBadRequest},
		{"file bad base64", "/_fake/files", map[string]any{"file_id": "abc", "content_base64": "!!"}, http.StatusBadRequest},
		{"fault ok", "/_fake/faults", map[string]any{"error_code": 429, "description": "Too Many Requests"}, http.StatusOK},
		{"fault 200 rejected", "/_fake/faults", map[string]any{"error_code": 200, "description": "x"}, http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			if status, body := f.admin(t, http.MethodPost, tt.path, tt.body); status != tt.wantStatus {
				t.Fatalf("status = %d, want %d (%v)", status, tt.wantStatus, body)
			}
		})
	}
}

func TestAdminResetAndClearFaults(t *testing.T) {
	f := newFixture(t)
	f.seedGroup(t, "member")
	f.admin(t, http.MethodPost, "/_fake/faults", map[string]any{"error_code": 500, "description": "boom", "times": 5})
	if status, _ := f.call(t, testToken, "sendMessage", map[string]any{"chat_id": groupID, "text": "x"}); status != 500 {
		t.Fatalf("fault not applied: %d", status)
	}
	f.admin(t, http.MethodDelete, "/_fake/faults", nil)
	if status, _ := f.call(t, testToken, "sendMessage", map[string]any{"chat_id": groupID, "text": "x"}); status != http.StatusOK {
		t.Fatalf("send after clearing faults: %d", status)
	}
	f.admin(t, http.MethodPost, "/_fake/reset", nil)
	if status, _ := f.call(t, testToken, "sendMessage", map[string]any{"chat_id": groupID, "text": "x"}); status != http.StatusBadRequest {
		t.Fatalf("chat survived reset: %d", status)
	}
	_, state := f.admin(t, http.MethodGet, "/_fake/state", nil)
	if bot, _ := state["bot"].(map[string]any); bot["username"] != "quoker_test_bot" {
		t.Fatalf("bot identity lost on reset: %v", state["bot"])
	}
}

func TestAdminHealthAndSentList(t *testing.T) {
	f := newFixture(t)
	res, err := get(t, f.adminSrv.URL+"/healthz")
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("healthz = %d", res.StatusCode)
	}
	res, err = get(t, f.adminSrv.URL+"/_fake/sent")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("sent = %d", res.StatusCode)
	}
}
