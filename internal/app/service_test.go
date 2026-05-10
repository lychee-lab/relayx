package app

import (
	"context"
	"strings"
	"testing"

	"github.com/lychee-lab/relayx/internal/core"
)

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
