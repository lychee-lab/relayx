package core

import "testing"

func TestParseStartCommand(t *testing.T) {
	cmd, err := ParseCommand("/codex start repo=/tmp/demo fix the failing test")
	if err != nil {
		t.Fatal(err)
	}

	if cmd.Action != ActionStart {
		t.Fatalf("action = %q, want %q", cmd.Action, ActionStart)
	}
	if cmd.Repo != "/tmp/demo" {
		t.Fatalf("repo = %q", cmd.Repo)
	}
	if cmd.Text != "fix the failing test" {
		t.Fatalf("text = %q", cmd.Text)
	}
}

func TestParseSteerCommand(t *testing.T) {
	cmd, err := ParseCommand("/codex steer run tests after the patch")
	if err != nil {
		t.Fatal(err)
	}

	if cmd.Action != ActionSteer {
		t.Fatalf("action = %q, want %q", cmd.Action, ActionSteer)
	}
	if cmd.Text != "run tests after the patch" {
		t.Fatalf("text = %q", cmd.Text)
	}
}

func TestParseRejectsNonCodexCommand(t *testing.T) {
	if _, err := ParseCommand("codex status"); err == nil {
		t.Fatal("expected error")
	}
}

func TestParseStartRequiresRepo(t *testing.T) {
	if _, err := ParseCommand("/codex start fix bug"); err == nil {
		t.Fatal("expected error")
	}
}
