package feishu

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lychee-lab/relayx/internal/app"
	"github.com/lychee-lab/relayx/internal/codex"
	"github.com/lychee-lab/relayx/internal/core"
)

type callbackNotifier struct {
	messages []string
}

func (n *callbackNotifier) SendMessage(_ context.Context, _ string, text string) error {
	n.messages = append(n.messages, text)
	return nil
}

func (n *callbackNotifier) SendApproval(context.Context, string, core.Approval) error {
	return nil
}

func TestCallbackURLVerification(t *testing.T) {
	handler := CallbackHandler{VerificationToken: "verify-token"}
	req := callbackRequest(map[string]any{
		"type":      "url_verification",
		"token":     "verify-token",
		"challenge": "challenge-value",
	})
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "challenge-value") {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestCallbackMessageCreatesTask(t *testing.T) {
	notifier := &callbackNotifier{}
	service := app.NewService(core.NewTaskManager())
	handler := CallbackHandler{
		Service:           service,
		Notifier:          notifier,
		VerificationToken: "verify-token",
	}
	req := callbackRequest(map[string]any{
		"header": map[string]any{"event_type": "im.message.receive_v1", "token": "verify-token"},
		"event": map[string]any{
			"sender": map[string]any{"sender_id": map[string]any{"open_id": "ou_1"}},
			"message": map[string]any{
				"chat_id": "oc_1",
				"content": `{"text":"/codex start repo=/tmp/demo fix bug"}`,
			},
		},
	})
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if len(notifier.messages) != 1 || !strings.Contains(notifier.messages[0], "created task") {
		t.Fatalf("messages = %#v", notifier.messages)
	}
}

func TestCallbackMessageErrorIsVisible(t *testing.T) {
	notifier := &callbackNotifier{}
	service := app.NewService(core.NewTaskManager())
	handler := CallbackHandler{
		Service:           service,
		Notifier:          notifier,
		VerificationToken: "verify-token",
	}
	req := callbackRequest(map[string]any{
		"header": map[string]any{"event_type": "im.message.receive_v1", "token": "verify-token"},
		"event": map[string]any{
			"sender": map[string]any{"sender_id": map[string]any{"open_id": "ou_1"}},
			"message": map[string]any{
				"chat_id": "oc_1",
				"content": `{"text":"hello"}`,
			},
		},
	})
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if len(notifier.messages) != 1 || !strings.Contains(notifier.messages[0], "/codex help") {
		t.Fatalf("messages = %#v", notifier.messages)
	}
	if !strings.Contains(rec.Body.String(), "command must start with /codex") {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestCallbackCardActionResolvesApproval(t *testing.T) {
	tasks := core.NewTaskManager()
	task, err := tasks.Start("oc_1", "ou_1", "/tmp/demo", "fix bug")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tasks.MarkStarted(task.ID, "thread-1", "turn-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := tasks.AddApproval(task.ID, "approval-1", "thread-1", "turn-1", "item/commandExecution/requestApproval", "npm test", time.Minute); err != nil {
		t.Fatal(err)
	}

	service := app.NewService(tasks)
	handler := CallbackHandler{
		Service:           service,
		VerificationToken: "verify-token",
	}
	req := callbackRequest(map[string]any{
		"header": map[string]any{"event_type": "card.action.trigger", "token": "verify-token"},
		"event": map[string]any{
			"operator": map[string]any{"operator_id": map[string]any{"open_id": "ou_1"}},
			"action": map[string]any{"value": map[string]any{
				"approval_id": "approval-1",
				"decision":    string(codex.ApprovalApproved),
			}},
		},
	})
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	approval, ok := tasks.ApprovalByID("approval-1")
	if !ok || approval.Status != core.ApprovalApproved {
		t.Fatalf("approval = %#v ok=%v", approval, ok)
	}
}

func callbackRequest(payload map[string]any) *http.Request {
	data, _ := json.Marshal(payload)
	return httptest.NewRequest(http.MethodPost, "/feishu/events", bytes.NewReader(data))
}
