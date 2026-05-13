package app

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/lychee-lab/relayx/internal/codex"
	"github.com/lychee-lab/relayx/internal/core"
)

type fakeCodexAdapter struct {
	events           chan codex.Event
	approvals        chan codex.ApprovalRequest
	steerText        string
	approvalID       string
	approvalDecision codex.ApprovalDecision
	mu               sync.Mutex
}

func newFakeCodexAdapter() *fakeCodexAdapter {
	return &fakeCodexAdapter{
		events:    make(chan codex.Event, 8),
		approvals: make(chan codex.ApprovalRequest, 8),
	}
}

func (f *fakeCodexAdapter) StartThread(context.Context, codex.StartThreadRequest) (codex.ThreadID, error) {
	return "thread-1", nil
}

func (f *fakeCodexAdapter) StartTurn(context.Context, codex.StartTurnRequest) (codex.TurnID, error) {
	return "turn-1", nil
}

func (f *fakeCodexAdapter) SteerTurn(_ context.Context, _ codex.ThreadID, _ codex.TurnID, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.steerText = text
	return nil
}

func (f *fakeCodexAdapter) RespondApproval(_ context.Context, approvalID string, decision codex.ApprovalDecision) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.approvalID = approvalID
	f.approvalDecision = decision
	return nil
}

func (f *fakeCodexAdapter) Events() <-chan codex.Event {
	return f.events
}

func (f *fakeCodexAdapter) Approvals() <-chan codex.ApprovalRequest {
	return f.approvals
}

func (f *fakeCodexAdapter) Close() error {
	close(f.events)
	close(f.approvals)
	return nil
}

type fakeNotifier struct {
	mu        sync.Mutex
	messages  []string
	markdown  []markdownMessage
	approvals []core.Approval
}

type markdownMessage struct {
	title string
	text  string
}

func (f *fakeNotifier) SendMessage(_ context.Context, _ string, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.messages = append(f.messages, text)
	return nil
}

func (f *fakeNotifier) SendMarkdown(_ context.Context, _ string, title string, markdown string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.markdown = append(f.markdown, markdownMessage{title: title, text: markdown})
	return nil
}

func (f *fakeNotifier) SendApproval(_ context.Context, _ string, approval core.Approval) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.approvals = append(f.approvals, approval)
	return nil
}

func TestServiceStartsSteersAndApprovesCodexTask(t *testing.T) {
	fakeCodex := newFakeCodexAdapter()
	notifier := &fakeNotifier{}
	service := NewService(core.NewTaskManager(), WithCodex(fakeCodex), WithNotifier(notifier), WithApprovalTTL(time.Minute))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go service.Run(ctx)

	reply, err := service.HandleMessage(ctx, InboundMessage{
		ChatID: "oc_1",
		UserID: "ou_1",
		Text:   "/codex start repo=/tmp/demo fix bug",
	})
	if err != nil {
		t.Fatal(err)
	}
	if reply.Task == nil || reply.Task.ThreadID != "thread-1" || reply.Task.Status != core.TaskRunning {
		t.Fatalf("reply task = %#v", reply.Task)
	}

	if _, err := service.HandleMessage(ctx, InboundMessage{
		ChatID: "oc_1",
		UserID: "ou_1",
		Text:   "/codex steer run tests",
	}); err != nil {
		t.Fatal(err)
	}
	if fakeCodex.steerText != "run tests" {
		t.Fatalf("steer text = %q", fakeCodex.steerText)
	}

	fakeCodex.approvals <- codex.ApprovalRequest{
		ID:       "approval-1",
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		Kind:     "item/commandExecution/requestApproval",
		Summary:  "npm test token=secret",
	}

	waitFor(t, func() bool {
		notifier.mu.Lock()
		defer notifier.mu.Unlock()
		return len(notifier.approvals) == 1
	})

	notifier.mu.Lock()
	approval := notifier.approvals[0]
	notifier.mu.Unlock()
	if approval.Summary == "npm test token=secret" {
		t.Fatal("approval summary was not redacted")
	}

	if _, err := service.HandleApproval(ctx, "ou_1", "approval-1", codex.ApprovalApproved); err != nil {
		t.Fatal(err)
	}
	if fakeCodex.approvalID != "approval-1" || fakeCodex.approvalDecision != codex.ApprovalApproved {
		t.Fatalf("approval response = %q %q", fakeCodex.approvalID, fakeCodex.approvalDecision)
	}
}

func TestServiceHandlesCodexCompletionEvent(t *testing.T) {
	fakeCodex := newFakeCodexAdapter()
	notifier := &fakeNotifier{}
	service := NewService(core.NewTaskManager(), WithCodex(fakeCodex), WithNotifier(notifier))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go service.Run(ctx)

	reply, err := service.HandleMessage(ctx, InboundMessage{
		ChatID: "oc_1",
		UserID: "ou_1",
		Text:   "/codex start repo=/tmp/demo fix bug",
	})
	if err != nil {
		t.Fatal(err)
	}

	fakeCodex.events <- codex.Event{ThreadID: "thread-1", Kind: "turn/completed", Message: "done"}
	waitFor(t, func() bool {
		task, ok := service.TaskByID(reply.Task.ID)
		return ok && task.Status == core.TaskCompleted
	})
	waitFor(t, func() bool {
		notifier.mu.Lock()
		defer notifier.mu.Unlock()
		return len(notifier.markdown) == 1
	})

	notifier.mu.Lock()
	message := notifier.markdown[0]
	notifier.mu.Unlock()
	if message.title != "Codex task task-000001 completed" || message.text != "done" {
		t.Fatalf("message = %#v", message)
	}
}

func TestServiceSuppressesNoisyCodexDeltaEvent(t *testing.T) {
	fakeCodex := newFakeCodexAdapter()
	notifier := &fakeNotifier{}
	service := NewService(core.NewTaskManager(), WithCodex(fakeCodex), WithNotifier(notifier))

	ctx := context.Background()
	reply, err := service.HandleMessage(ctx, InboundMessage{
		ChatID: "oc_1",
		UserID: "ou_1",
		Text:   "/codex start repo=/tmp/demo fix bug",
	})
	if err != nil {
		t.Fatal(err)
	}

	service.handleCodexEvent(ctx, codex.Event{
		ThreadID: "thread-1",
		Kind:     "item/agentMessage/delta",
		Message:  "item/agentMessage/delta",
	})

	task, ok := service.TaskByID(reply.Task.ID)
	if !ok {
		t.Fatalf("task %q not found", reply.Task.ID)
	}
	if task.Status != core.TaskRunning || task.LastEvent != "" {
		t.Fatalf("task = %#v", task)
	}
	notifier.mu.Lock()
	defer notifier.mu.Unlock()
	if len(notifier.messages) != 0 {
		t.Fatalf("messages = %#v", notifier.messages)
	}
}

func TestServiceDoesNotNotifyGenericRunningCodexEvent(t *testing.T) {
	fakeCodex := newFakeCodexAdapter()
	notifier := &fakeNotifier{}
	service := NewService(core.NewTaskManager(), WithCodex(fakeCodex), WithNotifier(notifier))

	ctx := context.Background()
	reply, err := service.HandleMessage(ctx, InboundMessage{
		ChatID: "oc_1",
		UserID: "ou_1",
		Text:   "/codex start repo=/tmp/demo fix bug",
	})
	if err != nil {
		t.Fatal(err)
	}

	service.handleCodexEvent(ctx, codex.Event{
		ThreadID: "thread-1",
		Kind:     "item/commandExecution/started",
		Message:  "item/commandExecution/started",
	})

	task, ok := service.TaskByID(reply.Task.ID)
	if !ok {
		t.Fatalf("task %q not found", reply.Task.ID)
	}
	if task.Status != core.TaskRunning || task.LastEvent != "item/commandExecution/started" {
		t.Fatalf("task = %#v", task)
	}
	notifier.mu.Lock()
	defer notifier.mu.Unlock()
	if len(notifier.messages) != 0 {
		t.Fatalf("messages = %#v", notifier.messages)
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
