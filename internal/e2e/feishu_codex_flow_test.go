package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/lychee-lab/relayx/internal/app"
	"github.com/lychee-lab/relayx/internal/codex"
	"github.com/lychee-lab/relayx/internal/core"
	"github.com/lychee-lab/relayx/internal/httpapi"
)

func TestFeishuToCodexApprovalEndToEnd(t *testing.T) {
	fakeCodex := &e2eCodex{
		events:    make(chan codex.Event, 8),
		approvals: make(chan codex.ApprovalRequest, 8),
	}
	notifier := &e2eNotifier{}
	service := app.NewService(
		core.NewTaskManager(),
		app.WithCodex(fakeCodex),
		app.WithNotifier(notifier),
		app.WithApprovalTTL(time.Minute),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go service.Run(ctx)

	server := httptest.NewServer(httpapi.NewHandler(service, notifier, "verify-token"))
	defer server.Close()

	postJSON(t, server, "/feishu/events", map[string]any{
		"header": map[string]any{"event_type": "im.message.receive_v1", "token": "verify-token"},
		"event": map[string]any{
			"sender": map[string]any{"sender_id": map[string]any{"open_id": "ou_1"}},
			"message": map[string]any{
				"chat_id": "oc_1",
				"content": `{"text":"/codex start repo=/tmp/demo fix failing test"}`,
			},
		},
	})

	waitFor(t, func() bool {
		notifier.mu.Lock()
		defer notifier.mu.Unlock()
		return len(notifier.messages) == 1
	})

	fakeCodex.approvals <- codex.ApprovalRequest{
		ID:       "approval-1",
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		Kind:     "item/commandExecution/requestApproval",
		Summary:  "npm test",
	}

	waitFor(t, func() bool {
		notifier.mu.Lock()
		defer notifier.mu.Unlock()
		return len(notifier.approvals) == 1
	})

	postJSON(t, server, "/feishu/events", map[string]any{
		"header": map[string]any{"event_type": "card.action.trigger", "token": "verify-token"},
		"event": map[string]any{
			"operator": map[string]any{"operator_id": map[string]any{"open_id": "ou_1"}},
			"action": map[string]any{"value": map[string]any{
				"approval_id": "approval-1",
				"decision":    string(codex.ApprovalApproved),
			}},
		},
	})

	waitFor(t, func() bool {
		fakeCodex.mu.Lock()
		defer fakeCodex.mu.Unlock()
		return fakeCodex.approvalID == "approval-1" && fakeCodex.decision == codex.ApprovalApproved
	})
}

type e2eCodex struct {
	events     chan codex.Event
	approvals  chan codex.ApprovalRequest
	mu         sync.Mutex
	approvalID string
	decision   codex.ApprovalDecision
}

func (f *e2eCodex) StartThread(context.Context, codex.StartThreadRequest) (codex.ThreadID, error) {
	return "thread-1", nil
}

func (f *e2eCodex) StartTurn(context.Context, codex.StartTurnRequest) (codex.TurnID, error) {
	return "turn-1", nil
}

func (f *e2eCodex) SteerTurn(context.Context, codex.ThreadID, codex.TurnID, string) error {
	return nil
}

func (f *e2eCodex) StartReview(context.Context, codex.StartReviewRequest) (codex.StartReviewResponse, error) {
	return codex.StartReviewResponse{ReviewThreadID: "thread-1", TurnID: "review-turn-1"}, nil
}

func (f *e2eCodex) ListModels(context.Context) ([]codex.ModelInfo, error) {
	return nil, nil
}

func (f *e2eCodex) ListThreads(context.Context, codex.ListThreadsRequest) ([]codex.ThreadInfo, error) {
	return []codex.ThreadInfo{{ID: "thread-1", Name: "demo", CWD: "/tmp/demo"}}, nil
}

func (f *e2eCodex) ResumeThread(context.Context, codex.ResumeThreadRequest) (codex.ResumeThreadResponse, error) {
	return codex.ResumeThreadResponse{Thread: codex.ThreadInfo{ID: "thread-1", Name: "demo", CWD: "/tmp/demo"}, CWD: "/tmp/demo"}, nil
}

func (f *e2eCodex) RespondApproval(_ context.Context, approvalID string, decision codex.ApprovalDecision) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.approvalID = approvalID
	f.decision = decision
	return nil
}

func (f *e2eCodex) Events() <-chan codex.Event {
	return f.events
}

func (f *e2eCodex) Approvals() <-chan codex.ApprovalRequest {
	return f.approvals
}

func (f *e2eCodex) Close() error {
	close(f.events)
	close(f.approvals)
	return nil
}

type e2eNotifier struct {
	mu        sync.Mutex
	messages  []string
	approvals []core.Approval
	resumes   [][]core.ResumeOption
}

func (n *e2eNotifier) SendMessage(context.Context, string, string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.messages = append(n.messages, "message")
	return nil
}

func (n *e2eNotifier) SendApproval(_ context.Context, _ string, approval core.Approval) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.approvals = append(n.approvals, approval)
	return nil
}

func (n *e2eNotifier) SendResumeOptions(_ context.Context, _ string, options []core.ResumeOption) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.resumes = append(n.resumes, options)
	return nil
}

func postJSON(t *testing.T, server *httptest.Server, path string, payload map[string]any) {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := server.Client().Post(server.URL+path, "application/json", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST %s status = %d", path, resp.StatusCode)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not reached")
}
