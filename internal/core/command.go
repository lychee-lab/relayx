package core

import (
	"errors"
	"fmt"
	"strconv"
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
	ActionModel  Action = "model"
	ActionFast   Action = "fast"
	ActionReview Action = "review"
	ActionResume Action = "resume"
)

var ErrUnsupportedCommand = errors.New("command must start with /codex")

type Command struct {
	Action         Action `json:"action"`
	Subcommand     string `json:"subcommand,omitempty"`
	Repo           string `json:"repo,omitempty"`
	Text           string `json:"text,omitempty"`
	Model          string `json:"model,omitempty"`
	Effort         string `json:"effort,omitempty"`
	ReviewTarget   string `json:"review_target,omitempty"`
	ReviewBase     string `json:"review_base,omitempty"`
	ReviewCommit   string `json:"review_commit,omitempty"`
	ReviewDelivery string `json:"review_delivery,omitempty"`
	ResumeThreadID string `json:"resume_thread_id,omitempty"`
	Limit          int    `json:"limit,omitempty"`
}

func ParseCommand(input string) (Command, error) {
	fields := strings.Fields(strings.TrimSpace(input))
	if len(fields) == 0 {
		return Command{}, errors.New("empty command")
	}

	switch fields[0] {
	case "/model":
		return parseModel(fields[1:])
	case "/fast":
		return parseFast(fields[1:])
	case "/review":
		return parseReview(fields[1:])
	case "/resume":
		return parseResume(fields[1:])
	case "/codex":
	default:
		return Command{}, ErrUnsupportedCommand
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
	case ActionModel:
		return parseModel(args)
	case ActionFast:
		return parseFast(args)
	case ActionReview:
		return parseReview(args)
	case ActionResume:
		return parseResume(args)
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

func IsUnsupportedCommand(err error) bool {
	return errors.Is(err, ErrUnsupportedCommand)
}

func parseResume(args []string) (Command, error) {
	cmd := Command{Action: ActionResume, Subcommand: "list"}
	if len(args) == 0 {
		return cmd, nil
	}

	for _, arg := range args {
		key, value, ok := strings.Cut(arg, "=")
		if !ok {
			switch strings.ToLower(arg) {
			case "list", "ls":
				cmd.Subcommand = "list"
			case "current", "show":
				cmd.Subcommand = "current"
			default:
				cmd.Subcommand = "select"
				cmd.ResumeThreadID = arg
			}
			continue
		}

		switch strings.ToLower(key) {
		case "thread", "thread_id", "id":
			cmd.Subcommand = "select"
			cmd.ResumeThreadID = value
		case "repo", "cwd":
			cmd.Repo = value
		case "limit":
			limit, err := strconv.Atoi(value)
			if err != nil || limit <= 0 {
				return Command{}, fmt.Errorf("resume limit must be a positive integer")
			}
			cmd.Limit = limit
		default:
			return Command{}, fmt.Errorf("unknown resume option %q", key)
		}
	}

	if cmd.Subcommand == "select" && cmd.ResumeThreadID == "" {
		return Command{}, errors.New("resume requires a thread id")
	}
	return cmd, nil
}

func parseStart(args []string) (Command, error) {
	if len(args) == 0 {
		return Command{}, errors.New("start requires repo=/path and prompt text")
	}

	var repo string
	var model string
	var effort string
	promptParts := make([]string, 0, len(args))
	for _, arg := range args {
		switch {
		case strings.HasPrefix(arg, "repo="):
			repo = strings.TrimPrefix(arg, "repo=")
		case strings.HasPrefix(arg, "model="):
			model = strings.TrimPrefix(arg, "model=")
		case strings.HasPrefix(arg, "effort="):
			var err error
			effort, err = parseEffort(strings.TrimPrefix(arg, "effort="))
			if err != nil {
				return Command{}, err
			}
		case strings.HasPrefix(arg, "reasoning="):
			var err error
			effort, err = parseEffort(strings.TrimPrefix(arg, "reasoning="))
			if err != nil {
				return Command{}, err
			}
		default:
			promptParts = append(promptParts, arg)
		}
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
		Model:  model,
		Effort: effort,
	}, nil
}

func parseModel(args []string) (Command, error) {
	if len(args) == 0 {
		return Command{Action: ActionModel, Subcommand: "current"}, nil
	}

	if len(args) == 1 {
		switch strings.ToLower(args[0]) {
		case "list", "ls":
			return Command{Action: ActionModel, Subcommand: "list"}, nil
		case "current", "show":
			return Command{Action: ActionModel, Subcommand: "current"}, nil
		}
	}

	cmd := Command{Action: ActionModel, Subcommand: "set"}
	for _, arg := range args {
		key, value, ok := strings.Cut(arg, "=")
		if !ok {
			if cmd.Model == "" {
				if effort, err := parseEffort(arg); err == nil {
					cmd.Effort = effort
				} else {
					cmd.Model = arg
				}
				continue
			}
			effort, err := parseEffort(arg)
			if err != nil {
				return Command{}, fmt.Errorf("unknown model option %q", arg)
			}
			cmd.Effort = effort
			continue
		}

		switch strings.ToLower(key) {
		case "model":
			cmd.Model = value
		case "effort", "reasoning":
			effort, err := parseEffort(value)
			if err != nil {
				return Command{}, err
			}
			cmd.Effort = effort
		default:
			return Command{}, fmt.Errorf("unknown model option %q", key)
		}
	}
	if cmd.Model == "" && cmd.Effort == "" {
		return Command{}, errors.New("model requires a model id, effort, list, or current")
	}
	return cmd, nil
}

func parseFast(args []string) (Command, error) {
	cmd := Command{Action: ActionFast, Subcommand: "set", Effort: "low"}
	for _, arg := range args {
		key, value, ok := strings.Cut(arg, "=")
		if !ok {
			if effort, err := parseEffort(arg); err == nil {
				cmd.Effort = effort
				continue
			}
			if cmd.Model == "" {
				cmd.Model = arg
				continue
			}
			return Command{}, fmt.Errorf("unknown fast option %q", arg)
		}
		switch strings.ToLower(key) {
		case "model":
			cmd.Model = value
		case "effort", "reasoning":
			effort, err := parseEffort(value)
			if err != nil {
				return Command{}, err
			}
			cmd.Effort = effort
		default:
			return Command{}, fmt.Errorf("unknown fast option %q", key)
		}
	}
	return cmd, nil
}

func parseReview(args []string) (Command, error) {
	cmd := Command{
		Action:         ActionReview,
		ReviewTarget:   "uncommittedChanges",
		ReviewDelivery: "inline",
	}
	custom := make([]string, 0, len(args))
	for _, arg := range args {
		key, value, ok := strings.Cut(arg, "=")
		if !ok {
			switch strings.ToLower(arg) {
			case "detached":
				cmd.ReviewDelivery = "detached"
			case "inline":
				cmd.ReviewDelivery = "inline"
			default:
				custom = append(custom, arg)
			}
			continue
		}
		switch strings.ToLower(key) {
		case "base", "branch":
			cmd.ReviewTarget = "baseBranch"
			cmd.ReviewBase = value
		case "commit", "sha":
			cmd.ReviewTarget = "commit"
			cmd.ReviewCommit = value
		case "delivery":
			if value != "inline" && value != "detached" {
				return Command{}, fmt.Errorf("review delivery must be inline or detached")
			}
			cmd.ReviewDelivery = value
		case "target":
			switch value {
			case "uncommittedChanges":
				cmd.ReviewTarget = value
			default:
				return Command{}, fmt.Errorf("unsupported review target %q", value)
			}
		default:
			return Command{}, fmt.Errorf("unknown review option %q", key)
		}
	}
	if len(custom) > 0 {
		cmd.ReviewTarget = "custom"
		cmd.Text = strings.Join(custom, " ")
	}
	if cmd.ReviewTarget == "baseBranch" && cmd.ReviewBase == "" {
		return Command{}, errors.New("review base requires base=<branch>")
	}
	if cmd.ReviewTarget == "commit" && cmd.ReviewCommit == "" {
		return Command{}, errors.New("review commit requires commit=<sha>")
	}
	return cmd, nil
}

func parseEffort(value string) (string, error) {
	switch strings.ToLower(value) {
	case "none", "minimal", "low", "medium", "high", "xhigh":
		return strings.ToLower(value), nil
	default:
		return "", fmt.Errorf("unsupported effort %q", value)
	}
}

func HelpText() string {
	return strings.Join([]string{
		"Usage:",
		"/codex start repo=/path/to/repo task description",
		"/codex start repo=/path/to/repo model=gpt-5.2 effort=high task description",
		"/codex status",
		"/codex steer additional instruction",
		"/model list",
		"/model <model-id> [effort=low|medium|high|xhigh]",
		"/fast [model=<model-id>] [effort=low]",
		"/review [base=<branch>|commit=<sha>|detached]",
		"/resume [repo=/path] [limit=5]",
		"/codex stop",
		"/codex diff",
		"/codex logs",
	}, "\n")
}
