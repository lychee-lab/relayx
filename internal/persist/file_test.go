package persist

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lychee-lab/relayx/internal/app"
	"github.com/lychee-lab/relayx/internal/core"
)

func TestFileStateStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := &FileStateStore{Path: path}

	tasks := core.NewTaskManager()
	task, err := tasks.Start("oc_1", "ou_1", "/tmp/demo", "fix bug", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tasks.MarkStarted(task.ID, "thread-1", "turn-1"); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(context.Background(), tasks.Snapshot()); err != nil {
		t.Fatal(err)
	}

	snapshot, err := store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	restored := core.NewTaskManagerFromSnapshot(snapshot)
	got, ok := restored.ByThread("thread-1")
	if !ok {
		t.Fatal("expected restored task by thread")
	}
	if got.ID != task.ID {
		t.Fatalf("restored task id = %q, want %q", got.ID, task.ID)
	}
}

func TestFileAuditor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	auditor := &FileAuditor{Path: path}

	if err := auditor.Log(context.Background(), app.AuditEvent{
		At:     time.Now().UTC(),
		Actor:  "ou_1",
		Action: "message.start",
		Target: "/tmp/demo",
	}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "message.start") {
		t.Fatalf("audit file = %s", string(data))
	}
}
