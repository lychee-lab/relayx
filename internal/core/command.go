package core

import (
	"errors"
	"fmt"
	"strings"
)

type Action string

const (
	ActionStart  Action = "start"
	ActionStatus Action = "status"
	ActionSteer  Action = "steer"
	ActionStop   Action = "stop"
	ActionDiff   Action = "diff"
	ActionLogs   Action = "logs"
	ActionHelp   Action = "help"
)

type Command struct {
	Action Action `json:"action"`
	Repo   string `json:"repo,omitempty"`
	Text   string `json:"text,omitempty"`
}

func ParseCommand(input string) (Command, error) {
	fields := strings.Fields(strings.TrimSpace(input))
	if len(fields) == 0 {
		return Command{}, errors.New("empty command")
	}
	if fields[0] != "/codex" {
		return Command{}, fmt.Errorf("command must start with /codex")
	}
	if len(fields) == 1 {
		return Command{Action: ActionHelp}, nil
	}

	action := Action(strings.ToLower(fields[1]))
	args := fields[2:]

	switch action {
	case ActionStart:
		return parseStart(args)
	case ActionStatus, ActionStop, ActionDiff, ActionLogs, ActionHelp:
		if len(args) > 0 {
			return Command{}, fmt.Errorf("%s does not accept arguments", action)
		}
		return Command{Action: action}, nil
	case ActionSteer:
		text := strings.TrimSpace(strings.Join(args, " "))
		if text == "" {
			return Command{}, errors.New("steer requires instruction text")
		}
		return Command{Action: ActionSteer, Text: text}, nil
	default:
		return Command{}, fmt.Errorf("unknown /codex action %q", action)
	}
}

func parseStart(args []string) (Command, error) {
	if len(args) == 0 {
		return Command{}, errors.New("start requires repo=/path and prompt text")
	}

	var repo string
	promptParts := make([]string, 0, len(args))
	for _, arg := range args {
		if strings.HasPrefix(arg, "repo=") {
			repo = strings.TrimPrefix(arg, "repo=")
			continue
		}
		promptParts = append(promptParts, arg)
	}

	if repo == "" {
		return Command{}, errors.New("start requires repo=/path")
	}

	text := strings.TrimSpace(strings.Join(promptParts, " "))
	if text == "" {
		return Command{}, errors.New("start requires prompt text")
	}

	return Command{
		Action: ActionStart,
		Repo:   repo,
		Text:   text,
	}, nil
}

func HelpText() string {
	return strings.Join([]string{
		"Usage:",
		"/codex start repo=/path/to/repo task description",
		"/codex status",
		"/codex steer additional instruction",
		"/codex stop",
		"/codex diff",
		"/codex logs",
	}, "\n")
}
