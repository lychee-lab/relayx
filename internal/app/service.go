package app

import (
	"context"
	"fmt"
	"strings"
	"sync"
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
	SendResumeOptions(ctx context.Context, chatID string, options []core.ResumeOption) error
}

type MarkdownNotifier interface {
	SendMarkdown(ctx context.Context, chatID string, title string, markdown string) error
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
	tasks                 *core.TaskManager
	codex                 codex.Adapter
	notifier              Notifier
	policy                core.Policy
	approvalTTL           time.Duration
	state                 StateStore
	auditor               Auditor
	eventMu               sync.Mutex
	agentMessageDeltas    map[string]string
	completionNotifiedKey map[string]struct{}
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
		tasks:                 tasks,
		approvalTTL:           10 * time.Minute,
		agentMessageDeltas:    make(map[string]string),
		completionNotifiedKey: make(map[string]struct{}),
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
		settings, _ := s.tasks.ChatSettings(msg.ChatID)
		model := firstNonEmpty(cmd.Model, settings.Model)
		effort := firstNonEmpty(cmd.Effort, settings.Effort)
		task, err := s.tasks.Start(msg.ChatID, msg.UserID, cmd.Repo, cmd.Text, model, effort)
		if err != nil {
			return Reply{}, err
		}
		if s.codex != nil {
			threadID, err := s.codex.StartThread(ctx, codex.StartThreadRequest{
				CWD:            cmd.Repo,
				Model:          model,
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
				Model:    model,
				Effort:   effort,
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
	case core.ActionModel:
		_ = s.audit(ctx, msg.UserID, "message.model", msg.ChatID, map[string]any{"model": cmd.Model, "effort": cmd.Effort, "subcommand": cmd.Subcommand})
		if err := s.policy.Authorize(msg.UserID, ""); err != nil {
			return Reply{}, err
		}
		if cmd.Subcommand == "list" {
			if s.codex == nil {
				return Reply{}, fmt.Errorf("model list requires RELAYX_CODEX_MODE=app-server")
			}
			models, err := s.codex.ListModels(ctx)
			if err != nil {
				return Reply{}, err
			}
			return Reply{Text: formatModels(models)}, nil
		}
		if cmd.Subcommand == "current" {
			return Reply{Text: s.currentModelText(msg.ChatID)}, nil
		}
		return s.setModelOptions(ctx, msg.ChatID, cmd.Model, cmd.Effort, "model")
	case core.ActionFast:
		_ = s.audit(ctx, msg.UserID, "message.fast", msg.ChatID, map[string]any{"model": cmd.Model, "effort": cmd.Effort})
		if err := s.policy.Authorize(msg.UserID, ""); err != nil {
			return Reply{}, err
		}
		return s.setModelOptions(ctx, msg.ChatID, cmd.Model, cmd.Effort, "fast")
	case core.ActionReview:
		_ = s.audit(ctx, msg.UserID, "message.review", msg.ChatID, map[string]any{"target": cmd.ReviewTarget, "delivery": cmd.ReviewDelivery})
		if s.codex == nil {
			return Reply{}, fmt.Errorf("review requires RELAYX_CODEX_MODE=app-server")
		}
		task, ok := s.tasks.LatestByChat(msg.ChatID)
		if !ok {
			return Reply{}, fmt.Errorf("no task in this chat")
		}
		if err := s.policy.Authorize(msg.UserID, task.Repo); err != nil {
			return Reply{}, err
		}
		if task.ThreadID == "" {
			return Reply{}, fmt.Errorf("task %s has no codex thread", task.ID)
		}
		resp, err := s.codex.StartReview(ctx, codex.StartReviewRequest{
			ThreadID: codex.ThreadID(task.ThreadID),
			Target: codex.ReviewTarget{
				Type:         cmd.ReviewTarget,
				Branch:       cmd.ReviewBase,
				CommitSHA:    cmd.ReviewCommit,
				Instructions: cmd.Text,
			},
			Delivery: cmd.ReviewDelivery,
		})
		if err != nil {
			return Reply{}, err
		}
		if resp.TurnID != "" && (resp.ReviewThreadID == "" || string(resp.ReviewThreadID) == task.ThreadID) {
			if updated, err := s.tasks.SetLatestTurn(msg.ChatID, string(resp.TurnID)); err == nil {
				task = *updated
			}
		}
		if err := s.persist(ctx); err != nil {
			return Reply{}, err
		}
		return Reply{
			Text: fmt.Sprintf("started review for task %s", task.ID),
			Task: &task,
		}, nil
	case core.ActionResume:
		_ = s.audit(ctx, msg.UserID, "message.resume", msg.ChatID, map[string]any{"subcommand": cmd.Subcommand, "thread_id": cmd.ResumeThreadID, "repo": cmd.Repo})
		if s.codex == nil {
			return Reply{}, fmt.Errorf("resume requires RELAYX_CODEX_MODE=app-server")
		}
		if err := s.policy.Authorize(msg.UserID, cmd.Repo); err != nil {
			return Reply{}, err
		}
		if cmd.Repo == "" && len(s.policy.AllowedRepos) > 0 && cmd.Subcommand != "select" {
			return Reply{}, fmt.Errorf("resume list requires repo= when RELAYX_ALLOWED_REPOS is configured")
		}
		if cmd.Subcommand == "select" {
			return s.HandleResumeSelection(ctx, msg.UserID, msg.ChatID, cmd.ResumeThreadID, cmd.Repo)
		}
		limit := cmd.Limit
		if limit <= 0 {
			limit = 5
		}
		if limit > 10 {
			limit = 10
		}
		threads, err := s.codex.ListThreads(ctx, codex.ListThreadsRequest{CWD: cmd.Repo, Limit: limit})
		if err != nil {
			return Reply{}, err
		}
		options := resumeOptionsFromThreads(threads)
		if len(options) == 0 {
			return Reply{Text: "no resumable Codex sessions found"}, nil
		}
		if s.notifier != nil {
			if err := s.notifier.SendResumeOptions(ctx, msg.ChatID, options); err != nil {
				return Reply{}, err
			}
			return Reply{Text: fmt.Sprintf("sent %d resumable Codex sessions", len(options))}, nil
		}
		return Reply{Text: formatResumeOptions(options)}, nil
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
		if s.codex != nil && task.ThreadID != "" {
			if task.Status == core.TaskRunning || task.Status == core.TaskWaitingApproval {
				if task.TurnID != "" {
					if err := s.codex.SteerTurn(ctx, codex.ThreadID(task.ThreadID), codex.TurnID(task.TurnID), cmd.Text); err != nil {
						return Reply{}, err
					}
				}
			} else {
				turnID, err := s.codex.StartTurn(ctx, codex.StartTurnRequest{
					ThreadID: codex.ThreadID(task.ThreadID),
					Text:     cmd.Text,
					Model:    task.Model,
					Effort:   task.Effort,
				})
				if err != nil {
					return Reply{}, err
				}
				task, err = s.tasks.SetLatestTurn(msg.ChatID, string(turnID))
				if err != nil {
					return Reply{}, err
				}
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

func (s *Service) HandleResumeSelection(ctx context.Context, actorUserID string, chatID string, threadID string, cwdHint string) (Reply, error) {
	_ = s.audit(ctx, actorUserID, "resume.select", threadID, map[string]any{"chat_id": chatID, "cwd": cwdHint})
	if chatID == "" {
		return Reply{}, fmt.Errorf("chat_id is required")
	}
	if actorUserID == "" {
		return Reply{}, fmt.Errorf("user_id is required")
	}
	if threadID == "" {
		return Reply{}, fmt.Errorf("thread_id is required")
	}
	if cwdHint == "" && len(s.policy.AllowedRepos) > 0 {
		return Reply{}, fmt.Errorf("resume selection requires cwd when RELAYX_ALLOWED_REPOS is configured")
	}
	if err := s.policy.Authorize(actorUserID, cwdHint); err != nil {
		return Reply{}, err
	}
	if s.codex == nil {
		return Reply{}, fmt.Errorf("resume requires RELAYX_CODEX_MODE=app-server")
	}

	settings, _ := s.tasks.ChatSettings(chatID)
	resp, err := s.codex.ResumeThread(ctx, codex.ResumeThreadRequest{
		ThreadID:       codex.ThreadID(threadID),
		Model:          settings.Model,
		Effort:         settings.Effort,
		Sandbox:        "workspace-write",
		ApprovalPolicy: "on-request",
	})
	if err != nil {
		return Reply{}, err
	}
	repo := firstNonEmpty(resp.CWD, cwdHint, resp.Thread.CWD)
	if err := s.policy.Authorize(actorUserID, repo); err != nil {
		return Reply{}, err
	}
	model := firstNonEmpty(resp.Model, settings.Model)
	effort := firstNonEmpty(resp.Effort, settings.Effort)
	resumedThreadID := firstNonEmpty(resp.Thread.ID, threadID)
	task, err := s.tasks.Resume(chatID, actorUserID, repo, resumePrompt(resp.Thread), resumedThreadID, model, effort)
	if err != nil {
		return Reply{}, err
	}
	if err := s.persist(ctx); err != nil {
		return Reply{}, err
	}
	return Reply{
		Text: fmt.Sprintf("resumed Codex session %s as task %s", threadID, task.ID),
		Task: task,
	}, nil
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
	currentTask, ok := s.tasks.ByThread(string(event.ThreadID))
	if !ok {
		return
	}
	if event.Kind == "item/agentMessage/delta" {
		s.appendAgentMessageDelta(currentTask, event)
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

	if status == core.TaskRunning && isTerminalTaskStatus(currentTask.Status) {
		return
	}
	if status == core.TaskCompleted {
		event.Message = s.consumeFinalAgentMessage(currentTask, event)
		if s.completionNotificationSent(currentTask, event) {
			return
		}
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
	if status == core.TaskCompleted {
		if markdownNotifier, ok := s.notifier.(MarkdownNotifier); ok {
			if err := markdownNotifier.SendMarkdown(ctx, task.ChatID, fmt.Sprintf("Codex task %s completed", task.ID), message); err == nil {
				s.markCompletionNotificationSent(*task, event)
				return
			}
		}
	}
	_ = s.notifier.SendMessage(ctx, task.ChatID, message)
	if status == core.TaskCompleted {
		s.markCompletionNotificationSent(*task, event)
	}
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
		return message
	case core.TaskFailed:
		if message != "" {
			return fmt.Sprintf("Codex task %s failed: %s", taskID, message)
		}
		return fmt.Sprintf("Codex task %s failed: %s", taskID, event.Kind)
	default:
		return ""
	}
}

func (s *Service) appendAgentMessageDelta(task core.Task, event codex.Event) {
	message := event.Message
	if message == "" || message == event.Kind {
		return
	}

	s.eventMu.Lock()
	defer s.eventMu.Unlock()
	for _, key := range codexMessageKeys(task, event) {
		s.agentMessageDeltas[key] += message
	}
}

func (s *Service) consumeFinalAgentMessage(task core.Task, event codex.Event) string {
	message := event.Message
	if message == event.Kind {
		message = ""
	}

	s.eventMu.Lock()
	defer s.eventMu.Unlock()
	for _, key := range codexMessageKeys(task, event) {
		if accumulated := s.agentMessageDeltas[key]; strings.TrimSpace(message) == "" && strings.TrimSpace(accumulated) != "" {
			message = accumulated
		}
		delete(s.agentMessageDeltas, key)
	}
	return message
}

func (s *Service) completionNotificationSent(task core.Task, event codex.Event) bool {
	if strings.TrimSpace(event.Message) == "" || event.Message == event.Kind {
		return false
	}

	key := completionNotificationKey(task, event)
	s.eventMu.Lock()
	defer s.eventMu.Unlock()
	_, ok := s.completionNotifiedKey[key]
	return ok
}

func (s *Service) markCompletionNotificationSent(task core.Task, event codex.Event) {
	key := completionNotificationKey(task, event)
	s.eventMu.Lock()
	defer s.eventMu.Unlock()
	s.completionNotifiedKey[key] = struct{}{}
}

func completionNotificationKey(task core.Task, event codex.Event) string {
	turnID := string(event.TurnID)
	if turnID == "" {
		turnID = task.TurnID
	}
	if turnID == "" {
		turnID = string(event.ThreadID)
	}
	return task.ID + "\x00" + turnID
}

func codexMessageKeys(task core.Task, event codex.Event) []string {
	keys := make([]string, 0, 3)
	add := func(key string) {
		if key == "" {
			return
		}
		for _, existing := range keys {
			if existing == key {
				return
			}
		}
		keys = append(keys, key)
	}

	threadID := string(event.ThreadID)
	turnID := string(event.TurnID)
	if threadID != "" && turnID != "" {
		add(threadID + "\x00" + turnID)
	}
	if task.ThreadID != "" && task.TurnID != "" {
		add(task.ThreadID + "\x00" + task.TurnID)
	}
	add(threadID)
	add(task.ThreadID)
	return keys
}

func isTerminalTaskStatus(status core.TaskStatus) bool {
	switch status {
	case core.TaskCompleted, core.TaskFailed, core.TaskStopped:
		return true
	default:
		return false
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

func (s *Service) setModelOptions(ctx context.Context, chatID, model, effort, label string) (Reply, error) {
	settings, err := s.tasks.SetChatSettings(chatID, model, effort)
	if err != nil {
		return Reply{}, err
	}
	task, _ := s.tasks.SetLatestOptions(chatID, model, effort)
	if err := s.persist(ctx); err != nil {
		return Reply{}, err
	}

	text := fmt.Sprintf("%s settings updated for future turns: %s", label, formatModelSettings(settings.Model, settings.Effort))
	return Reply{Text: text, Task: task}, nil
}

func (s *Service) currentModelText(chatID string) string {
	settings, ok := s.tasks.ChatSettings(chatID)
	if !ok || (settings.Model == "" && settings.Effort == "") {
		task, hasTask := s.tasks.LatestByChat(chatID)
		if !hasTask || (task.Model == "" && task.Effort == "") {
			return "no model settings for this chat"
		}
		return "current model settings: " + formatModelSettings(task.Model, task.Effort)
	}
	return "current model settings: " + formatModelSettings(settings.Model, settings.Effort)
}

func formatModelSettings(model, effort string) string {
	parts := make([]string, 0, 2)
	if model != "" {
		parts = append(parts, "model="+model)
	}
	if effort != "" {
		parts = append(parts, "effort="+effort)
	}
	if len(parts) == 0 {
		return "default"
	}
	return strings.Join(parts, ", ")
}

func formatModels(models []codex.ModelInfo) string {
	if len(models) == 0 {
		return "no models returned by Codex"
	}

	var b strings.Builder
	b.WriteString("Available models:")
	limit := len(models)
	if limit > 20 {
		limit = 20
	}
	for i := 0; i < limit; i++ {
		model := models[i]
		id := firstNonEmpty(model.Model, model.ID)
		b.WriteString("\n")
		b.WriteString(id)
		if model.DisplayName != "" && model.DisplayName != id {
			b.WriteString(" - ")
			b.WriteString(model.DisplayName)
		}
		if model.DefaultReasoningEffort != "" {
			b.WriteString(" (default effort: ")
			b.WriteString(model.DefaultReasoningEffort)
			b.WriteString(")")
		}
		if len(model.SupportedEfforts) > 0 {
			b.WriteString(" efforts=")
			b.WriteString(strings.Join(model.SupportedEfforts, ","))
		}
	}
	if len(models) > limit {
		fmt.Fprintf(&b, "\n...and %d more", len(models)-limit)
	}
	return b.String()
}

func resumeOptionsFromThreads(threads []codex.ThreadInfo) []core.ResumeOption {
	options := make([]core.ResumeOption, 0, len(threads))
	for _, thread := range threads {
		if thread.ID == "" {
			continue
		}
		options = append(options, core.ResumeOption{
			ThreadID:      thread.ID,
			Title:         thread.Name,
			Preview:       thread.Preview,
			CWD:           thread.CWD,
			ModelProvider: thread.ModelProvider,
			UpdatedAt:     thread.UpdatedAt,
		})
	}
	return options
}

func formatResumeOptions(options []core.ResumeOption) string {
	if len(options) == 0 {
		return "no resumable Codex sessions found"
	}
	var b strings.Builder
	b.WriteString("Resumable Codex sessions:")
	for i, option := range options {
		fmt.Fprintf(&b, "\n%d. %s", i+1, option.ThreadID)
		title := firstNonEmpty(option.Title, option.Preview)
		if title != "" {
			b.WriteString(" - ")
			b.WriteString(truncate(title, 80))
		}
		if option.CWD != "" {
			b.WriteString("\n   repo: ")
			b.WriteString(option.CWD)
		}
	}
	b.WriteString("\nUse /resume <thread_id> to resume one.")
	return b.String()
}

func resumePrompt(thread codex.ThreadInfo) string {
	return firstNonEmpty(thread.Name, thread.Preview, "resumed Codex session")
}

func truncate(value string, max int) string {
	if max <= 0 || len(value) <= max {
		return value
	}
	if max <= 3 {
		return value[:max]
	}
	return value[:max-3] + "..."
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
