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
	startThreadReq   codex.StartThreadRequest
	startTurnReq     codex.StartTurnRequest
	reviewReq        codex.StartReviewRequest
	resumeReq        codex.ResumeThreadRequest
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

func (f *fakeCodexAdapter) StartThread(_ context.Context, req codex.StartThreadRequest) (codex.ThreadID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.startThreadReq = req
	return "thread-1", nil
}

func (f *fakeCodexAdapter) StartTurn(_ context.Context, req codex.StartTurnRequest) (codex.TurnID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.startTurnReq = req
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

func (f *fakeCodexAdapter) StartReview(_ context.Context, req codex.StartReviewRequest) (codex.StartReviewResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reviewReq = req
	return codex.StartReviewResponse{ReviewThreadID: req.ThreadID, TurnID: "review-turn-1"}, nil
}

func (f *fakeCodexAdapter) ListModels(context.Context) ([]codex.ModelInfo, error) {
	return []codex.ModelInfo{
		{ID: "gpt-5.2", Model: "gpt-5.2", DisplayName: "GPT-5.2", DefaultReasoningEffort: "medium"},
	}, nil
}

func (f *fakeCodexAdapter) ListThreads(context.Context, codex.ListThreadsRequest) ([]codex.ThreadInfo, error) {
	return []codex.ThreadInfo{
		{ID: "thread-resume-1", Name: "Fix failing test", Preview: "fix bug", CWD: "/tmp/demo", ModelProvider: "openai", UpdatedAt: 123},
	}, nil
}

func (f *fakeCodexAdapter) ResumeThread(_ context.Context, req codex.ResumeThreadRequest) (codex.ResumeThreadResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resumeReq = req
	return codex.ResumeThreadResponse{
		Thread: codex.ThreadInfo{ID: string(req.ThreadID), Name: "Fix failing test", Preview: "fix bug", CWD: "/tmp/demo", ModelProvider: "openai"},
		Model:  "gpt-5.2",
		CWD:    "/tmp/demo",
		Effort: "medium",
	}, nil
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
	resumes   [][]core.ResumeOption
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

func (f *fakeNotifier) SendResumeOptions(_ context.Context, _ string, options []core.ResumeOption) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resumes = append(f.resumes, append([]core.ResumeOption(nil), options...))
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

func TestServiceAppliesModelOptionsToFutureTurn(t *testing.T) {
	fakeCodex := newFakeCodexAdapter()
	service := NewService(core.NewTaskManager(), WithCodex(fakeCodex))

	ctx := context.Background()
	start, err := service.HandleMessage(ctx, InboundMessage{
		ChatID: "oc_1",
		UserID: "ou_1",
		Text:   "/codex start repo=/tmp/demo fix bug",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.HandleMessage(ctx, InboundMessage{
		ChatID: "oc_1",
		UserID: "ou_1",
		Text:   "/model gpt-5.2 effort=high",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.HandleMessage(ctx, InboundMessage{
		ChatID: "oc_1",
		UserID: "ou_1",
		Text:   "/fast",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.tasks.SetStatus(start.Task.ID, core.TaskCompleted, "turn/completed", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := service.HandleMessage(ctx, InboundMessage{
		ChatID: "oc_1",
		UserID: "ou_1",
		Text:   "/codex steer continue",
	}); err != nil {
		t.Fatal(err)
	}

	fakeCodex.mu.Lock()
	defer fakeCodex.mu.Unlock()
	if fakeCodex.startTurnReq.Model != "gpt-5.2" || fakeCodex.startTurnReq.Effort != "low" {
		t.Fatalf("start turn req = %#v", fakeCodex.startTurnReq)
	}
}

func TestServiceStartsReview(t *testing.T) {
	fakeCodex := newFakeCodexAdapter()
	service := NewService(core.NewTaskManager(), WithCodex(fakeCodex))

	ctx := context.Background()
	if _, err := service.HandleMessage(ctx, InboundMessage{
		ChatID: "oc_1",
		UserID: "ou_1",
		Text:   "/codex start repo=/tmp/demo fix bug",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.HandleMessage(ctx, InboundMessage{
		ChatID: "oc_1",
		UserID: "ou_1",
		Text:   "/review base=main detached",
	}); err != nil {
		t.Fatal(err)
	}

	fakeCodex.mu.Lock()
	defer fakeCodex.mu.Unlock()
	if fakeCodex.reviewReq.Target.Type != "baseBranch" || fakeCodex.reviewReq.Target.Branch != "main" || fakeCodex.reviewReq.Delivery != "detached" {
		t.Fatalf("review req = %#v", fakeCodex.reviewReq)
	}
}

func TestServiceListsAndResumesCodexThread(t *testing.T) {
	fakeCodex := newFakeCodexAdapter()
	notifier := &fakeNotifier{}
	service := NewService(core.NewTaskManager(), WithCodex(fakeCodex), WithNotifier(notifier))

	ctx := context.Background()
	reply, err := service.HandleMessage(ctx, InboundMessage{
		ChatID: "oc_1",
		UserID: "ou_1",
		Text:   "/resume",
	})
	if err != nil {
		t.Fatal(err)
	}
	if reply.Text != "sent 1 resumable Codex sessions" {
		t.Fatalf("reply = %#v", reply)
	}
	notifier.mu.Lock()
	resumes := len(notifier.resumes)
	notifier.mu.Unlock()
	if resumes != 1 {
		t.Fatalf("resume cards = %d", resumes)
	}

	resumed, err := service.HandleResumeSelection(ctx, "ou_1", "oc_1", "thread-resume-1", "/tmp/demo")
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Task == nil || resumed.Task.ThreadID != "thread-resume-1" || resumed.Task.Status != core.TaskResumed {
		t.Fatalf("resumed task = %#v", resumed.Task)
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
