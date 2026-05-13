package app

import (
	"context"
	"strings"
	"testing"

	"github.com/lychee-lab/relayx/internal/core"
)

type testAuditor struct {
	events []AuditEvent
}

func (a *testAuditor) Log(_ context.Context, event AuditEvent) error {
	a.events = append(a.events, event)
	return nil
}

func TestServiceHandlesTaskLifecycle(t *testing.T) {
	service := NewService(core.NewTaskManager())
	ctx := context.Background()

	start, err := service.HandleMessage(ctx, InboundMessage{
		ChatID: "oc_1",
		UserID: "ou_1",
		Text:   "/codex start repo=/tmp/demo fix the failing test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if start.Task == nil {
		t.Fatal("expected task")
	}
	if !strings.Contains(start.Text, start.Task.ID) {
		t.Fatalf("reply %q does not include task id %q", start.Text, start.Task.ID)
	}

	status, err := service.HandleMessage(ctx, InboundMessage{
		ChatID: "oc_1",
		UserID: "ou_1",
		Text:   "/codex status",
	})
	if err != nil {
		t.Fatal(err)
	}
	if status.Task == nil || status.Task.ID != start.Task.ID {
		t.Fatalf("status task = %#v, want %s", status.Task, start.Task.ID)
	}

	steer, err := service.HandleMessage(ctx, InboundMessage{
		ChatID: "oc_1",
		UserID: "ou_1",
		Text:   "/codex steer run tests after editing",
	})
	if err != nil {
		t.Fatal(err)
	}
	if steer.Task == nil || len(steer.Task.Instructions) != 1 {
		t.Fatalf("steer task = %#v", steer.Task)
	}

	stopped, err := service.HandleMessage(ctx, InboundMessage{
		ChatID: "oc_1",
		UserID: "ou_1",
		Text:   "/codex stop",
	})
	if err != nil {
		t.Fatal(err)
	}
	if stopped.Task == nil || stopped.Task.Status != core.TaskStopped {
		t.Fatalf("stopped task = %#v", stopped.Task)
	}
}

func TestServiceAuditsReceivedMessage(t *testing.T) {
	auditor := &testAuditor{}
	service := NewService(core.NewTaskManager(), WithAuditor(auditor))

	_, err := service.HandleMessage(context.Background(), InboundMessage{
		ChatID: "oc_1",
		UserID: "ou_1",
		Text:   "/codex help",
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(auditor.events) == 0 {
		t.Fatal("expected audit event")
	}
	event := auditor.events[0]
	if event.Action != "message.received" || event.Actor != "ou_1" || event.Target != "oc_1" {
		t.Fatalf("event = %#v", event)
	}
	if event.Payload["text"] != "/codex help" {
		t.Fatalf("payload = %#v", event.Payload)
	}
}

func TestServiceRejectsMissingActor(t *testing.T) {
	service := NewService(core.NewTaskManager())

	_, err := service.HandleMessage(context.Background(), InboundMessage{
		ChatID: "oc_1",
		Text:   "/codex status",
	})
	if err == nil {
		t.Fatal("expected error")
	}
}
