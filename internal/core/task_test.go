package core

import (
	"fmt"
	"testing"
	"time"
)

func TestTaskManagerStartAndStatus(t *testing.T) {
	tasks := NewTaskManager()

	task, err := tasks.Start("oc_1", "ou_1", "/tmp/repo", "fix bug", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if task.ID == "" {
		t.Fatal("task id is empty")
	}

	latest, ok := tasks.LatestByChat("oc_1")
	if !ok {
		t.Fatal("expected latest task")
	}
	if latest.ID != task.ID {
		t.Fatalf("latest id = %q, want %q", latest.ID, task.ID)
	}
}

func TestTaskManagerAppendInstruction(t *testing.T) {
	tasks := NewTaskManager()
	if _, err := tasks.Start("oc_1", "ou_1", "/tmp/repo", "fix bug", "", ""); err != nil {
		t.Fatal(err)
	}

	task, err := tasks.AppendInstruction("oc_1", "run tests")
	if err != nil {
		t.Fatal(err)
	}
	if len(task.Instructions) != 1 || task.Instructions[0] != "run tests" {
		t.Fatalf("instructions = %#v", task.Instructions)
	}
}

func TestTaskManagerStopLatest(t *testing.T) {
	tasks := NewTaskManager()
	if _, err := tasks.Start("oc_1", "ou_1", "/tmp/repo", "fix bug", "", ""); err != nil {
		t.Fatal(err)
	}

	task, err := tasks.StopLatest("oc_1")
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != TaskStopped {
		t.Fatalf("status = %q, want %q", task.Status, TaskStopped)
	}
}

func TestTaskManagerResume(t *testing.T) {
	tasks := NewTaskManager()
	task, err := tasks.Resume("oc_1", "ou_1", "/tmp/repo", "resume bug fix", "thread-1", "gpt-5.2", "high")
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != TaskResumed || task.ThreadID != "thread-1" || task.Model != "gpt-5.2" || task.Effort != "high" {
		t.Fatalf("task = %#v", task)
	}
	latest, ok := tasks.LatestByChat("oc_1")
	if !ok || latest.ID != task.ID {
		t.Fatalf("latest = %#v ok=%v", latest, ok)
	}
}

func TestTaskManagerProcessedEventsPersistAndDedupe(t *testing.T) {
	tasks := NewTaskManager()
	at := time.Date(2026, 5, 14, 8, 0, 0, 0, time.UTC)

	if !tasks.MarkProcessedEvents([]string{"feishu:event:evt_1", "feishu:message:om_1"}, at) {
		t.Fatal("expected first event to be accepted")
	}
	if tasks.MarkProcessedEvents([]string{"feishu:event:evt_2", "feishu:message:om_1"}, at.Add(time.Second)) {
		t.Fatal("expected repeated message id to be rejected")
	}

	restored := NewTaskManagerFromSnapshot(tasks.Snapshot())
	if restored.MarkProcessedEvents([]string{"feishu:event:evt_3", "feishu:message:om_1"}, at.Add(2*time.Second)) {
		t.Fatal("expected restored manager to reject repeated message id")
	}
}

func TestTaskManagerProcessedEventsAreCapped(t *testing.T) {
	tasks := NewTaskManager()
	base := time.Date(2026, 5, 14, 8, 0, 0, 0, time.UTC)

	for i := 0; i < maxProcessedEvents+10; i++ {
		id := fmt.Sprintf("feishu:event:evt_%04d", i)
		if !tasks.MarkProcessedEvents([]string{id}, base.Add(time.Duration(i)*time.Second)) {
			t.Fatalf("event %q was rejected", id)
		}
	}

	snapshot := tasks.Snapshot()
	if len(snapshot.ProcessedEvents) != maxProcessedEvents {
		t.Fatalf("processed event count = %d, want %d", len(snapshot.ProcessedEvents), maxProcessedEvents)
	}
	if tasks.MarkProcessedEvents([]string{"feishu:event:evt_0010"}, base.Add(time.Hour)) {
		t.Fatal("expected newest capped set to keep recent duplicate history")
	}
	if !tasks.MarkProcessedEvents([]string{"feishu:event:evt_0000"}, base.Add(time.Hour)) {
		t.Fatal("expected oldest event outside cap to be accepted again")
	}
}
