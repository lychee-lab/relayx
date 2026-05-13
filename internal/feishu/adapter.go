package feishu

import (
	"context"

	"github.com/lychee-lab/relayx/internal/core"
)

type InboundMessage struct {
	ChatID string
	UserID string
	Text   string
}

type CardAction struct {
	ActionID string
	UserID   string
	ChatID   string
	Value    map[string]string
}

type Message struct {
	ChatID string
	Text   string
}

type ApprovalCard struct {
	ChatID     string
	ApprovalID string
	Title      string
	Summary    string
	Actions    []string
}

type ResumeCard struct {
	ChatID  string
	Options []core.ResumeOption
}

type Adapter interface {
	Start(ctx context.Context) error
	Messages() <-chan InboundMessage
	CardActions() <-chan CardAction
	SendMessage(ctx context.Context, msg Message) error
	SendApprovalCard(ctx context.Context, card ApprovalCard) error
	Close() error
}
