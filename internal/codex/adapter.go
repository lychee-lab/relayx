package codex

import "context"

type ThreadID string

type TurnID string

type ApprovalDecision string

const (
	ApprovalApproved        ApprovalDecision = "approved"
	ApprovalApprovedForTurn ApprovalDecision = "approved_for_session"
	ApprovalDenied          ApprovalDecision = "denied"
	ApprovalAbort           ApprovalDecision = "abort"
)

type StartThreadRequest struct {
	CWD            string
	Model          string
	Sandbox        string
	ApprovalPolicy string
}

type StartTurnRequest struct {
	ThreadID ThreadID
	Text     string
	Model    string
	Effort   string
}

type ReviewTarget struct {
	Type         string
	Branch       string
	CommitSHA    string
	Instructions string
}

type StartReviewRequest struct {
	ThreadID ThreadID
	Target   ReviewTarget
	Delivery string
}

type StartReviewResponse struct {
	ReviewThreadID ThreadID
	TurnID         TurnID
}

type ModelInfo struct {
	ID                     string
	Model                  string
	DisplayName            string
	DefaultReasoningEffort string
	SupportedEfforts       []string
	Hidden                 bool
}

type ThreadInfo struct {
	ID            string
	SessionID     string
	Name          string
	Preview       string
	CWD           string
	ModelProvider string
	UpdatedAt     int64
}

type ListThreadsRequest struct {
	CWD   string
	Limit int
}

type ResumeThreadRequest struct {
	ThreadID       ThreadID
	Model          string
	Effort         string
	Sandbox        string
	ApprovalPolicy string
}

type ResumeThreadResponse struct {
	Thread        ThreadInfo
	Model         string
	ModelProvider string
	CWD           string
	Effort        string
}

type ApprovalRequest struct {
	ID       string
	ThreadID ThreadID
	TurnID   TurnID
	Kind     string
	Summary  string
	Payload  map[string]any
}

type Event struct {
	ThreadID ThreadID
	TurnID   TurnID
	Kind     string
	Message  string
	Payload  map[string]any
}

type Adapter interface {
	StartThread(ctx context.Context, req StartThreadRequest) (ThreadID, error)
	StartTurn(ctx context.Context, req StartTurnRequest) (TurnID, error)
	SteerTurn(ctx context.Context, threadID ThreadID, turnID TurnID, text string) error
	StartReview(ctx context.Context, req StartReviewRequest) (StartReviewResponse, error)
	ListModels(ctx context.Context) ([]ModelInfo, error)
	ListThreads(ctx context.Context, req ListThreadsRequest) ([]ThreadInfo, error)
	ResumeThread(ctx context.Context, req ResumeThreadRequest) (ResumeThreadResponse, error)
	RespondApproval(ctx context.Context, approvalID string, decision ApprovalDecision) error
	Events() <-chan Event
	Approvals() <-chan ApprovalRequest
	Close() error
}
