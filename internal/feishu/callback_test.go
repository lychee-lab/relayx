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

func (n *callbackNotifier) SendResumeOptions(context.Context, string, []core.ResumeOption) error {
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

func TestCallbackCardActionResolvesApproval(t *testing.T) {
	tasks := core.NewTaskManager()
	task, err := tasks.Start("oc_1", "ou_1", "/tmp/demo", "fix bug", "", "")
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

func TestCallbackCardActionResumesThread(t *testing.T) {
	fakeCodex := &callbackCodex{
		events:    make(chan codex.Event),
		approvals: make(chan codex.ApprovalRequest),
	}
	service := app.NewService(core.NewTaskManager(), app.WithCodex(fakeCodex))
	handler := CallbackHandler{
		Service:           service,
		VerificationToken: "verify-token",
	}
	req := callbackRequest(map[string]any{
		"header": map[string]any{"event_type": "card.action.trigger", "token": "verify-token"},
		"event": map[string]any{
			"operator": map[string]any{"operator_id": map[string]any{"open_id": "ou_1"}},
			"action": map[string]any{"value": map[string]any{
				"action":    "resume_thread",
				"chat_id":   "oc_1",
				"thread_id": "thread-1",
				"cwd":       "/tmp/demo",
			}},
		},
	})
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if fakeCodex.resumeThreadID != "thread-1" {
		t.Fatalf("resume thread id = %q", fakeCodex.resumeThreadID)
	}
	task, ok := service.TaskByID("task-000001")
	if !ok || task.ThreadID != "thread-1" || task.Status != core.TaskResumed {
		t.Fatalf("task = %#v ok=%v", task, ok)
	}
}

type callbackCodex struct {
	events         chan codex.Event
	approvals      chan codex.ApprovalRequest
	resumeThreadID string
}

func (c *callbackCodex) StartThread(context.Context, codex.StartThreadRequest) (codex.ThreadID, error) {
	return "thread-1", nil
}

func (c *callbackCodex) StartTurn(context.Context, codex.StartTurnRequest) (codex.TurnID, error) {
	return "turn-1", nil
}

func (c *callbackCodex) SteerTurn(context.Context, codex.ThreadID, codex.TurnID, string) error {
	return nil
}

func (c *callbackCodex) StartReview(context.Context, codex.StartReviewRequest) (codex.StartReviewResponse, error) {
	return codex.StartReviewResponse{}, nil
}

func (c *callbackCodex) ListModels(context.Context) ([]codex.ModelInfo, error) {
	return nil, nil
}

func (c *callbackCodex) ListThreads(context.Context, codex.ListThreadsRequest) ([]codex.ThreadInfo, error) {
	return nil, nil
}

func (c *callbackCodex) ResumeThread(_ context.Context, req codex.ResumeThreadRequest) (codex.ResumeThreadResponse, error) {
	c.resumeThreadID = string(req.ThreadID)
	return codex.ResumeThreadResponse{
		Thread: codex.ThreadInfo{ID: string(req.ThreadID), Name: "demo", CWD: "/tmp/demo"},
		CWD:    "/tmp/demo",
	}, nil
}

func (c *callbackCodex) RespondApproval(context.Context, string, codex.ApprovalDecision) error {
	return nil
}

func (c *callbackCodex) Events() <-chan codex.Event {
	return c.events
}

func (c *callbackCodex) Approvals() <-chan codex.ApprovalRequest {
	return c.approvals
}

func (c *callbackCodex) Close() error {
	close(c.events)
	close(c.approvals)
	return nil
}

func callbackRequest(payload map[string]any) *http.Request {
	data, _ := json.Marshal(payload)
	return httptest.NewRequest(http.MethodPost, "/feishu/events", bytes.NewReader(data))
}
