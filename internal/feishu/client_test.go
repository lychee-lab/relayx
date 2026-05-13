package feishu

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientSendMessage(t *testing.T) {
	var seenAuth string
	var seenMessage map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/v3/tenant_access_token/internal":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code":                0,
				"msg":                 "ok",
				"tenant_access_token": "tenant-token",
				"expire":              7200,
			})
		case "/im/v1/messages":
			seenAuth = r.Header.Get("authorization")
			if err := json.NewDecoder(r.Body).Decode(&seenMessage); err != nil {
				t.Errorf("decode message: %v", err)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "msg": "ok", "data": map[string]any{"message_id": "om_1"}})
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := &Client{
		AppID:     "cli_a",
		AppSecret: "secret",
		BaseURL:   server.URL,
		HTTP:      server.Client(),
	}
	if err := client.SendMessage(context.Background(), Message{ChatID: "oc_1", Text: "hello token=secret"}); err != nil {
		t.Fatal(err)
	}
	if seenAuth != "Bearer tenant-token" {
		t.Fatalf("auth = %q", seenAuth)
	}
	if seenMessage["receive_id"] != "oc_1" || seenMessage["msg_type"] != "text" {
		t.Fatalf("message = %#v", seenMessage)
	}
	content := seenMessage["content"].(string)
	if content == "" || content == `{"text":"hello token=secret"}` {
		t.Fatalf("content was not redacted: %q", content)
	}
}

func TestClientSendMarkdownCard(t *testing.T) {
	var seenMessage map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/v3/tenant_access_token/internal":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code":                0,
				"msg":                 "ok",
				"tenant_access_token": "tenant-token",
				"expire":              7200,
			})
		case "/im/v1/messages":
			if err := json.NewDecoder(r.Body).Decode(&seenMessage); err != nil {
				t.Errorf("decode message: %v", err)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "msg": "ok", "data": map[string]any{"message_id": "om_1"}})
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := &Client{
		AppID:     "cli_a",
		AppSecret: "secret",
		BaseURL:   server.URL,
		HTTP:      server.Client(),
	}
	err := client.SendMarkdownCard(context.Background(), MarkdownCard{
		ChatID:   "oc_1",
		Title:    "Codex result",
		Markdown: "**done**\n\n```go\nsecret := \"token=secret\"\n```",
	})
	if err != nil {
		t.Fatal(err)
	}
	if seenMessage["receive_id"] != "oc_1" || seenMessage["msg_type"] != "interactive" {
		t.Fatalf("message = %#v", seenMessage)
	}
	content := seenMessage["content"].(string)
	var card map[string]any
	if err := json.Unmarshal([]byte(content), &card); err != nil {
		t.Fatal(err)
	}
	elements := card["elements"].([]any)
	markdown := elements[0].(map[string]any)
	if markdown["tag"] != "markdown" {
		t.Fatalf("markdown element = %#v", markdown)
	}
	if strings.Contains(markdown["content"].(string), "token=secret") {
		t.Fatalf("markdown content was not redacted: %q", markdown["content"])
	}
}
