package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/lychee-lab/relayx/internal/codex"
	"github.com/lychee-lab/relayx/internal/core"
)

type InboundMessage struct {
	ChatID string `json:"chat_id"`
	UserID string `json:"user_id"`
	Text   string `json:"text"`
}

type Reply struct {
	Text     string                 `json:"text"`
	Task     *core.Task             `json:"task,omitempty"`
	Approval *core.Approval         `json:"approval,omitempty"`
	Decision codex.ApprovalDecision `json:"decision,omitempty"`
}

type Notifier interface {
	SendMessage(ctx context.Context, chatID string, text string) error
	SendApproval(ctx context.Context, chatID string, approval core.Approval) error
}

type StateStore interface {
	Save(ctx context.Context, snapshot core.Snapshot) error
}

type AuditEvent struct {
	At      time.Time      `json:"at"`
	Actor   string         `json:"actor,omitempty"`
	Action  string         `json:"action"`
	Target  string         `json:"target,omitempty"`
	Payload map[string]any `json:"payload,omitempty"`
}

type Auditor interface {
	Log(ctx context.Context, event AuditEvent) error
}

type Service struct {
	tasks       *core.TaskManager
	codex       codex.Adapter
	notifier    Notifier
	policy      core.Policy
	approvalTTL time.Duration
	state       StateStore
	auditor     Auditor
}

type Option func(*Service)

func WithCodex(adapter codex.Adapter) Option {
	return func(s *Service) {
		s.codex = adapter
	}
}

func WithNotifier(notifier Notifier) Option {
	return func(s *Service) {
		s.notifier = notifier
	}
}

func WithPolicy(policy core.Policy) Option {
	return func(s *Service) {
		s.policy = policy
	}
}

func WithApprovalTTL(ttl time.Duration) Option {
	return func(s *Service) {
		s.approvalTTL = ttl
	}
}

func WithStateStore(state StateStore) Option {
	return func(s *Service) {
		s.state = state
	}
}

func WithAuditor(auditor Auditor) Option {
	return func(s *Service) {
		s.auditor = auditor
	}
}

func NewService(tasks *core.TaskManager, opts ...Option) *Service {
	service := &Service{
		tasks:       tasks,
		approvalTTL: 10 * time.Minute,
	}
	for _, opt := range opts {
		opt(service)
	}
	return service
}

func (s *Service) TaskByID(taskID string) (core.Task, bool) {
	return s.tasks.ByID(taskID)
}

func (s *Service) Run(ctx context.Context) {
	if s.codex == nil {
		<-ctx.Done()
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-s.codex.Events():
			if !ok {
				return
			}
			s.handleCodexEvent(ctx, event)
		case approval, ok := <-s.codex.Approvals():
			if !ok {
				return
			}
			s.handleCodexApproval(ctx, approval)
		}
	}
}

func (s *Service) HandleMessage(ctx context.Context, msg InboundMessage) (Reply, error) {
	if msg.ChatID == "" {
		return Reply{}, fmt.Errorf("chat_id is required")
	}
	if msg.UserID == "" {
		return Reply{}, fmt.Errorf("user_id is required")
	}

	_ = s.audit(ctx, msg.UserID, "message.received", msg.ChatID, map[string]any{"text": core.RedactSecrets(msg.Text)})

	cmd, err := core.ParseCommand(msg.Text)
	if err != nil {
		return Reply{}, err
	}

	switch cmd.Action {
	case core.ActionStart:
		_ = s.audit(ctx, msg.UserID, "message.start", cmd.Repo, map[string]any{"chat_id": msg.ChatID})
		if err := s.policy.Authorize(msg.UserID, cmd.Repo); err != nil {
			return Reply{}, err
		}
		task, err := s.tasks.Start(msg.ChatID, msg.UserID, cmd.Repo, cmd.Text)
		if err != nil {
			return Reply{}, err
		}
		if s.codex != nil {
			threadID, err := s.codex.StartThread(ctx, codex.StartThreadRequest{
				CWD:            cmd.Repo,
				Sandbox:        "workspace-write",
				ApprovalPolicy: "on-request",
			})
			if err != nil {
				_, _ = s.tasks.SetStatus(task.ID, core.TaskFailed, "thread/start", err.Error())
				return Reply{}, err
			}
			turnID, err := s.codex.StartTurn(ctx, codex.StartTurnRequest{
				ThreadID: threadID,
				Text:     cmd.Text,
			})
			if err != nil {
				return Reply{}, err
			}
			task, err = s.tasks.MarkStarted(task.ID, string(threadID), string(turnID))
			if err != nil {
				return Reply{}, err
			}
		}
		if err := s.persist(ctx); err != nil {
			return Reply{}, err
		}
		return Reply{
			Text: fmt.Sprintf("created task %s for repo %s", task.ID, task.Repo),
			Task: task,
		}, nil
	case core.ActionStatus:
		task, ok := s.tasks.LatestByChat(msg.ChatID)
		if !ok {
			return Reply{Text: "no task in this chat"}, nil
		}
		return Reply{
			Text: fmt.Sprintf("task %s is %s", task.ID, task.Status),
			Task: &task,
		}, nil
	case core.ActionSteer:
		_ = s.audit(ctx, msg.UserID, "message.steer", msg.ChatID, map[string]any{"text": cmd.Text})
		task, err := s.tasks.AppendInstruction(msg.ChatID, cmd.Text)
		if err != nil {
			return Reply{}, err
		}
		if s.codex != nil && task.ThreadID != "" && task.TurnID != "" {
			if err := s.codex.SteerTurn(ctx, codex.ThreadID(task.ThreadID), codex.TurnID(task.TurnID), cmd.Text); err != nil {
				return Reply{}, err
			}
		}
		if err := s.persist(ctx); err != nil {
			return Reply{}, err
		}
		return Reply{
			Text: fmt.Sprintf("queued instruction for task %s", task.ID),
			Task: task,
		}, nil
	case core.ActionStop:
		_ = s.audit(ctx, msg.UserID, "message.stop", msg.ChatID, nil)
		task, err := s.tasks.StopLatest(msg.ChatID)
		if err != nil {
			return Reply{}, err
		}
		if err := s.persist(ctx); err != nil {
			return Reply{}, err
		}
		return Reply{
			Text: fmt.Sprintf("stopped task %s", task.ID),
			Task: task,
		}, nil
	case core.ActionDiff, core.ActionLogs:
		task, ok := s.tasks.LatestByChat(msg.ChatID)
		if !ok {
			return Reply{Text: "no task in this chat"}, nil
		}
		return Reply{
			Text: fmt.Sprintf("%s is not wired yet for task %s", cmd.Action, task.ID),
			Task: &task,
		}, nil
	case core.ActionHelp:
		return Reply{Text: core.HelpText()}, nil
	default:
		return Reply{}, fmt.Errorf("unsupported action %q", cmd.Action)
	}
}

func (s *Service) HandleApproval(ctx context.Context, actorUserID string, approvalID string, decision codex.ApprovalDecision) (Reply, error) {
	_ = s.audit(ctx, actorUserID, "approval.resolve", approvalID, map[string]any{"decision": string(decision)})
	approval, ok := s.tasks.ApprovalByID(approvalID)
	if !ok {
		return Reply{}, fmt.Errorf("approval %q not found", approvalID)
	}

	task, ok := s.tasks.ByID(approval.TaskID)
	if !ok {
		return Reply{}, fmt.Errorf("task %q not found", approval.TaskID)
	}
	if err := s.policy.Authorize(actorUserID, task.Repo); err != nil {
		return Reply{}, err
	}
	if time.Now().UTC().After(approval.ExpiresAt) {
		resolved, err := s.tasks.ResolveApproval(approvalID, core.ApprovalExpired)
		if err != nil {
			return Reply{}, err
		}
		_ = s.persist(ctx)
		return Reply{Text: fmt.Sprintf("approval %s expired", approvalID), Approval: resolved}, nil
	}

	if s.codex != nil {
		if err := s.codex.RespondApproval(ctx, approvalID, decision); err != nil {
			return Reply{}, err
		}
	}

	status := core.ApprovalDenied
	switch decision {
	case codex.ApprovalApproved, codex.ApprovalApprovedForTurn:
		status = core.ApprovalApproved
	case codex.ApprovalAbort:
		status = core.ApprovalAborted
	}
	resolved, err := s.tasks.ResolveApproval(approvalID, status)
	if err != nil {
		return Reply{}, err
	}
	if err := s.persist(ctx); err != nil {
		return Reply{}, err
	}
	return Reply{
		Text:     fmt.Sprintf("approval %s resolved as %s", approvalID, decision),
		Approval: resolved,
		Decision: decision,
	}, nil
}

func (s *Service) handleCodexEvent(ctx context.Context, event codex.Event) {
	if event.ThreadID == "" {
		return
	}
	if isNoisyCodexEvent(event.Kind) {
		return
	}

	status := core.TaskRunning
	errText := ""
	switch event.Kind {
	case "turn/completed":
		status = core.TaskCompleted
	case "error", "protocol/error":
		status = core.TaskFailed
		errText = event.Message
	}

	task, ok := s.tasks.SetStatusByThread(string(event.ThreadID), status, event.Kind, errText)
	if !ok || s.notifier == nil {
		_ = s.persist(ctx)
		return
	}
	_ = s.persist(ctx)
	message := codexEventNotification(task.ID, status, event)
	if message == "" {
		return
	}
	_ = s.notifier.SendMessage(ctx, task.ChatID, message)
}

func isNoisyCodexEvent(kind string) bool {
	return strings.HasSuffix(kind, "/delta") || strings.Contains(kind, ".delta")
}

func codexEventNotification(taskID string, status core.TaskStatus, event codex.Event) string {
	message := core.RedactSecrets(event.Message)
	if message == "" || message == event.Kind {
		message = ""
	}

	switch status {
	case core.TaskCompleted:
		if message != "" {
			return fmt.Sprintf("Codex task %s completed: %s", taskID, message)
		}
		return fmt.Sprintf("Codex task %s completed.", taskID)
	case core.TaskFailed:
		if message != "" {
			return fmt.Sprintf("Codex task %s failed: %s", taskID, message)
		}
		return fmt.Sprintf("Codex task %s failed: %s", taskID, event.Kind)
	default:
		return ""
	}
}

func (s *Service) handleCodexApproval(ctx context.Context, req codex.ApprovalRequest) {
	if req.ThreadID == "" {
		return
	}
	task, ok := s.tasks.ByThread(string(req.ThreadID))
	if !ok {
		return
	}

	summary := core.RedactSecrets(req.Summary)
	approval, err := s.tasks.AddApproval(task.ID, req.ID, string(req.ThreadID), string(req.TurnID), req.Kind, summary, s.approvalTTL)
	if err != nil || s.notifier == nil {
		_ = s.persist(ctx)
		return
	}
	_ = s.persist(ctx)
	_ = s.notifier.SendApproval(ctx, task.ChatID, *approval)
}

func (s *Service) persist(ctx context.Context) error {
	if s.state == nil {
		return nil
	}
	return s.state.Save(ctx, s.tasks.Snapshot())
}

func (s *Service) audit(ctx context.Context, actor, action, target string, payload map[string]any) error {
	if s.auditor == nil {
		return nil
	}
	return s.auditor.Log(ctx, AuditEvent{
		At:      time.Now().UTC(),
		Actor:   actor,
		Action:  action,
		Target:  target,
		Payload: payload,
	})
}
