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

func TestParseDirectModelCommand(t *testing.T) {
	cmd, err := ParseCommand("/model gpt-5.2 effort=high")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Action != ActionModel || cmd.Model != "gpt-5.2" || cmd.Effort != "high" {
		t.Fatalf("cmd = %#v", cmd)
	}
}

func TestParseFastCommand(t *testing.T) {
	cmd, err := ParseCommand("/fast model=gpt-5.4-mini")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Action != ActionFast || cmd.Model != "gpt-5.4-mini" || cmd.Effort != "low" {
		t.Fatalf("cmd = %#v", cmd)
	}
}

func TestParseReviewCommand(t *testing.T) {
	cmd, err := ParseCommand("/review base=main detached")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Action != ActionReview || cmd.ReviewTarget != "baseBranch" || cmd.ReviewBase != "main" || cmd.ReviewDelivery != "detached" {
		t.Fatalf("cmd = %#v", cmd)
	}
}

func TestParseResumeCommand(t *testing.T) {
	cmd, err := ParseCommand("/resume repo=/tmp/demo limit=3")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Action != ActionResume || cmd.Subcommand != "list" || cmd.Repo != "/tmp/demo" || cmd.Limit != 3 {
		t.Fatalf("cmd = %#v", cmd)
	}

	cmd, err = ParseCommand("/codex resume thread-1")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Action != ActionResume || cmd.Subcommand != "select" || cmd.ResumeThreadID != "thread-1" {
		t.Fatalf("cmd = %#v", cmd)
	}
}
