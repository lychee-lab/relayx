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
	RespondApproval(ctx context.Context, approvalID string, decision ApprovalDecision) error
	Events() <-chan Event
	Approvals() <-chan ApprovalRequest
	Close() error
}
