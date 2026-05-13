package core

import (
	"fmt"
	"sync"
	"time"
)

type TaskStatus string

const (
	TaskCreated         TaskStatus = "created"
	TaskRunning         TaskStatus = "running"
	TaskResumed         TaskStatus = "resumed"
	TaskWaitingApproval TaskStatus = "waiting_approval"
	TaskCompleted       TaskStatus = "completed"
	TaskFailed          TaskStatus = "failed"
	TaskStopped         TaskStatus = "stopped"
)

type Task struct {
	ID           string     `json:"id"`
	ChatID       string     `json:"chat_id"`
	UserID       string     `json:"user_id"`
	Repo         string     `json:"repo"`
	Prompt       string     `json:"prompt"`
	Model        string     `json:"model,omitempty"`
	Effort       string     `json:"effort,omitempty"`
	ThreadID     string     `json:"thread_id,omitempty"`
	TurnID       string     `json:"turn_id,omitempty"`
	Instructions []string   `json:"instructions,omitempty"`
	Status       TaskStatus `json:"status"`
	LastEvent    string     `json:"last_event,omitempty"`
	Error        string     `json:"error,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

type ApprovalStatus string

const (
	ApprovalPending  ApprovalStatus = "pending"
	ApprovalApproved ApprovalStatus = "approved"
	ApprovalDenied   ApprovalStatus = "denied"
	ApprovalAborted  ApprovalStatus = "aborted"
	ApprovalExpired  ApprovalStatus = "expired"
)

type Approval struct {
	ID         string         `json:"id"`
	TaskID     string         `json:"task_id"`
	ThreadID   string         `json:"thread_id,omitempty"`
	TurnID     string         `json:"turn_id,omitempty"`
	Kind       string         `json:"kind"`
	Summary    string         `json:"summary"`
	Status     ApprovalStatus `json:"status"`
	CreatedAt  time.Time      `json:"created_at"`
	ExpiresAt  time.Time      `json:"expires_at"`
	ResolvedAt *time.Time     `json:"resolved_at,omitempty"`
}

type ChatSettings struct {
	ChatID string `json:"chat_id"`
	Model  string `json:"model,omitempty"`
	Effort string `json:"effort,omitempty"`
}

type ResumeOption struct {
	ThreadID      string `json:"thread_id"`
	Title         string `json:"title,omitempty"`
	Preview       string `json:"preview,omitempty"`
	CWD           string `json:"cwd,omitempty"`
	ModelProvider string `json:"model_provider,omitempty"`
	UpdatedAt     int64  `json:"updated_at,omitempty"`
}

type TaskManager struct {
	mu             sync.Mutex
	nextTask       int64
	tasks          map[string]*Task
	latestByChat   map[string]string
	taskByThread   map[string]string
	approvals      map[string]*Approval
	approvalByTask map[string][]string
	chatSettings   map[string]*ChatSettings
}

type Snapshot struct {
	NextTask     int64          `json:"next_task"`
	Tasks        []Task         `json:"tasks"`
	Approvals    []Approval     `json:"approvals"`
	ChatSettings []ChatSettings `json:"chat_settings,omitempty"`
}

func NewTaskManager() *TaskManager {
	return &TaskManager{
		tasks:          make(map[string]*Task),
		latestByChat:   make(map[string]string),
		taskByThread:   make(map[string]string),
		approvals:      make(map[string]*Approval),
		approvalByTask: make(map[string][]string),
		chatSettings:   make(map[string]*ChatSettings),
	}
}

func NewTaskManagerFromSnapshot(snapshot Snapshot) *TaskManager {
	manager := NewTaskManager()
	manager.nextTask = snapshot.NextTask
	for i := range snapshot.Tasks {
		task := snapshot.Tasks[i]
		manager.tasks[task.ID] = cloneTask(&task)
		manager.latestByChat[task.ChatID] = task.ID
		if task.ThreadID != "" {
			manager.taskByThread[task.ThreadID] = task.ID
		}
	}
	for i := range snapshot.Approvals {
		approval := snapshot.Approvals[i]
		manager.approvals[approval.ID] = cloneApproval(&approval)
		manager.approvalByTask[approval.TaskID] = append(manager.approvalByTask[approval.TaskID], approval.ID)
	}
	for i := range snapshot.ChatSettings {
		settings := snapshot.ChatSettings[i]
		manager.chatSettings[settings.ChatID] = cloneChatSettings(&settings)
	}
	return manager
}

func (m *TaskManager) Snapshot() Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()

	snapshot := Snapshot{
		NextTask:     m.nextTask,
		Tasks:        make([]Task, 0, len(m.tasks)),
		Approvals:    make([]Approval, 0, len(m.approvals)),
		ChatSettings: make([]ChatSettings, 0, len(m.chatSettings)),
	}
	for _, task := range m.tasks {
		snapshot.Tasks = append(snapshot.Tasks, *cloneTask(task))
	}
	for _, approval := range m.approvals {
		snapshot.Approvals = append(snapshot.Approvals, *cloneApproval(approval))
	}
	for _, settings := range m.chatSettings {
		snapshot.ChatSettings = append(snapshot.ChatSettings, *cloneChatSettings(settings))
	}
	return snapshot
}

func (m *TaskManager) Start(chatID, userID, repo, prompt, model, effort string) (*Task, error) {
	if chatID == "" {
		return nil, fmt.Errorf("chatID is required")
	}
	if userID == "" {
		return nil, fmt.Errorf("userID is required")
	}
	if repo == "" {
		return nil, fmt.Errorf("repo is required")
	}
	if prompt == "" {
		return nil, fmt.Errorf("prompt is required")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.nextTask++
	now := time.Now().UTC()
	task := &Task{
		ID:        fmt.Sprintf("task-%06d", m.nextTask),
		ChatID:    chatID,
		UserID:    userID,
		Repo:      repo,
		Prompt:    prompt,
		Model:     model,
		Effort:    effort,
		Status:    TaskCreated,
		CreatedAt: now,
		UpdatedAt: now,
	}
	m.tasks[task.ID] = task
	m.latestByChat[chatID] = task.ID

	return cloneTask(task), nil
}

func (m *TaskManager) Resume(chatID, userID, repo, prompt, threadID, model, effort string) (*Task, error) {
	if chatID == "" {
		return nil, fmt.Errorf("chatID is required")
	}
	if userID == "" {
		return nil, fmt.Errorf("userID is required")
	}
	if repo == "" {
		return nil, fmt.Errorf("repo is required")
	}
	if threadID == "" {
		return nil, fmt.Errorf("threadID is required")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.nextTask++
	now := time.Now().UTC()
	task := &Task{
		ID:        fmt.Sprintf("task-%06d", m.nextTask),
		ChatID:    chatID,
		UserID:    userID,
		Repo:      repo,
		Prompt:    prompt,
		Model:     model,
		Effort:    effort,
		ThreadID:  threadID,
		Status:    TaskResumed,
		CreatedAt: now,
		UpdatedAt: now,
	}
	m.tasks[task.ID] = task
	m.latestByChat[chatID] = task.ID
	m.taskByThread[threadID] = task.ID

	return cloneTask(task), nil
}

func (m *TaskManager) ChatSettings(chatID string) (ChatSettings, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	settings, ok := m.chatSettings[chatID]
	if !ok {
		return ChatSettings{}, false
	}
	return *cloneChatSettings(settings), true
}

func (m *TaskManager) SetChatSettings(chatID, model, effort string) (ChatSettings, error) {
	if chatID == "" {
		return ChatSettings{}, fmt.Errorf("chatID is required")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	settings, ok := m.chatSettings[chatID]
	if !ok {
		settings = &ChatSettings{ChatID: chatID}
		m.chatSettings[chatID] = settings
	}
	if model != "" {
		settings.Model = model
	}
	if effort != "" {
		settings.Effort = effort
	}
	return *cloneChatSettings(settings), nil
}

func (m *TaskManager) SetLatestOptions(chatID, model, effort string) (*Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	task, err := m.latestByChatLocked(chatID)
	if err != nil {
		return nil, err
	}
	if model != "" {
		task.Model = model
	}
	if effort != "" {
		task.Effort = effort
	}
	task.UpdatedAt = time.Now().UTC()
	return cloneTask(task), nil
}

func (m *TaskManager) MarkStarted(taskID, threadID, turnID string) (*Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	task, err := m.taskLocked(taskID)
	if err != nil {
		return nil, err
	}
	task.ThreadID = threadID
	task.TurnID = turnID
	task.Status = TaskRunning
	task.UpdatedAt = time.Now().UTC()
	if threadID != "" {
		m.taskByThread[threadID] = task.ID
	}
	return cloneTask(task), nil
}

func (m *TaskManager) LatestByChat(chatID string) (Task, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	id, ok := m.latestByChat[chatID]
	if !ok {
		return Task{}, false
	}
	task, ok := m.tasks[id]
	if !ok {
		return Task{}, false
	}
	return *cloneTask(task), true
}

func (m *TaskManager) ByID(taskID string) (Task, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	task, ok := m.tasks[taskID]
	if !ok {
		return Task{}, false
	}
	return *cloneTask(task), true
}

func (m *TaskManager) ByThread(threadID string) (Task, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	taskID, ok := m.taskByThread[threadID]
	if !ok {
		return Task{}, false
	}
	task, ok := m.tasks[taskID]
	if !ok {
		return Task{}, false
	}
	return *cloneTask(task), true
}

func (m *TaskManager) SetStatus(taskID string, status TaskStatus, event string, errText string) (*Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	task, err := m.taskLocked(taskID)
	if err != nil {
		return nil, err
	}
	task.Status = status
	task.LastEvent = event
	task.Error = errText
	task.UpdatedAt = time.Now().UTC()
	return cloneTask(task), nil
}

func (m *TaskManager) AppendInstruction(chatID, text string) (*Task, error) {
	if text == "" {
		return nil, fmt.Errorf("instruction text is required")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	task, err := m.latestByChatLocked(chatID)
	if err != nil {
		return nil, err
	}
	task.Instructions = append(task.Instructions, text)
	task.UpdatedAt = time.Now().UTC()
	return cloneTask(task), nil
}

func (m *TaskManager) SetLatestTurn(chatID, turnID string) (*Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	task, err := m.latestByChatLocked(chatID)
	if err != nil {
		return nil, err
	}
	task.TurnID = turnID
	task.Status = TaskRunning
	task.UpdatedAt = time.Now().UTC()
	return cloneTask(task), nil
}

func (m *TaskManager) StopLatest(chatID string) (*Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	task, err := m.latestByChatLocked(chatID)
	if err != nil {
		return nil, err
	}
	task.Status = TaskStopped
	task.UpdatedAt = time.Now().UTC()
	return cloneTask(task), nil
}

func (m *TaskManager) SetStatusByThread(threadID string, status TaskStatus, event string, errText string) (*Task, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	taskID, ok := m.taskByThread[threadID]
	if !ok {
		return nil, false
	}
	task, ok := m.tasks[taskID]
	if !ok {
		return nil, false
	}
	task.Status = status
	task.LastEvent = event
	task.Error = errText
	task.UpdatedAt = time.Now().UTC()
	return cloneTask(task), true
}

func (m *TaskManager) AddApproval(taskID, approvalID, threadID, turnID, kind, summary string, ttl time.Duration) (*Approval, error) {
	if approvalID == "" {
		return nil, fmt.Errorf("approval id is required")
	}
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	task, err := m.taskLocked(taskID)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	approval := &Approval{
		ID:        approvalID,
		TaskID:    task.ID,
		ThreadID:  threadID,
		TurnID:    turnID,
		Kind:      kind,
		Summary:   summary,
		Status:    ApprovalPending,
		CreatedAt: now,
		ExpiresAt: now.Add(ttl),
	}
	m.approvals[approval.ID] = approval
	m.approvalByTask[task.ID] = append(m.approvalByTask[task.ID], approval.ID)
	task.Status = TaskWaitingApproval
	task.UpdatedAt = now
	return cloneApproval(approval), nil
}

func (m *TaskManager) ResolveApproval(approvalID string, status ApprovalStatus) (*Approval, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	approval, ok := m.approvals[approvalID]
	if !ok {
		return nil, fmt.Errorf("approval %q not found", approvalID)
	}
	if approval.Status != ApprovalPending {
		return cloneApproval(approval), nil
	}
	now := time.Now().UTC()
	approval.Status = status
	approval.ResolvedAt = &now

	if task, ok := m.tasks[approval.TaskID]; ok {
		if status == ApprovalApproved {
			task.Status = TaskRunning
		}
		if status == ApprovalAborted {
			task.Status = TaskStopped
		}
		task.UpdatedAt = now
	}

	return cloneApproval(approval), nil
}

func (m *TaskManager) ApprovalByID(approvalID string) (Approval, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	approval, ok := m.approvals[approvalID]
	if !ok {
		return Approval{}, false
	}
	return *cloneApproval(approval), true
}

func (m *TaskManager) PendingApprovals(taskID string) []Approval {
	m.mu.Lock()
	defer m.mu.Unlock()

	ids := m.approvalByTask[taskID]
	out := make([]Approval, 0, len(ids))
	for _, id := range ids {
		approval := m.approvals[id]
		if approval != nil && approval.Status == ApprovalPending {
			out = append(out, *cloneApproval(approval))
		}
	}
	return out
}

func (m *TaskManager) taskLocked(taskID string) (*Task, error) {
	task, ok := m.tasks[taskID]
	if !ok {
		return nil, fmt.Errorf("task %q not found", taskID)
	}
	return task, nil
}

func (m *TaskManager) latestByChatLocked(chatID string) (*Task, error) {
	id, ok := m.latestByChat[chatID]
	if !ok {
		return nil, fmt.Errorf("no task in chat %q", chatID)
	}
	task, ok := m.tasks[id]
	if !ok {
		return nil, fmt.Errorf("latest task %q not found", id)
	}
	return task, nil
}

func cloneTask(task *Task) *Task {
	cp := *task
	if task.Instructions != nil {
		cp.Instructions = append([]string(nil), task.Instructions...)
	}
	return &cp
}

func cloneApproval(approval *Approval) *Approval {
	cp := *approval
	if approval.ResolvedAt != nil {
		resolvedAt := *approval.ResolvedAt
		cp.ResolvedAt = &resolvedAt
	}
	return &cp
}

func cloneChatSettings(settings *ChatSettings) *ChatSettings {
	cp := *settings
	return &cp
}
