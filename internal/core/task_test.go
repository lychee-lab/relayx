package core

import "testing"

func TestTaskManagerStartAndStatus(t *testing.T) {
	tasks := NewTaskManager()

	task, err := tasks.Start("oc_1", "ou_1", "/tmp/repo", "fix bug")
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
	if _, err := tasks.Start("oc_1", "ou_1", "/tmp/repo", "fix bug"); err != nil {
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
	if _, err := tasks.Start("oc_1", "ou_1", "/tmp/repo", "fix bug"); err != nil {
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
