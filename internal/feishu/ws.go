package feishu

import (
	"context"
	"fmt"
	"log"
	"strings"

	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"
	"github.com/lychee-lab/relayx/internal/app"
	"github.com/lychee-lab/relayx/internal/codex"
	"github.com/lychee-lab/relayx/internal/core"
)

type WSReceiver struct {
	AppID             string
	AppSecret         string
	BaseURL           string
	VerificationToken string
	Service           *app.Service
	Notifier          app.Notifier
}

func (r WSReceiver) Start(ctx context.Context) error {
	if r.AppID == "" || r.AppSecret == "" {
		return fmt.Errorf("feishu app id and secret are required")
	}
	if r.Service == nil {
		return fmt.Errorf("service is required")
	}

	eventHandler := dispatcher.NewEventDispatcher(r.VerificationToken, "").
		OnP2MessageReceiveV1(r.handleP2MessageReceiveV1).
		OnP2CardActionTrigger(r.handleP2CardActionTrigger)

	log.Printf("feishu long connection receiver starting domain=%s", feishuWSDomain(r.BaseURL))
	client := larkws.NewClient(
		r.AppID,
		r.AppSecret,
		larkws.WithEventHandler(eventHandler),
		larkws.WithDomain(feishuWSDomain(r.BaseURL)),
		larkws.WithLogLevel(larkcore.LogLevelInfo),
	)
	return client.Start(ctx)
}

func (r WSReceiver) handleP2MessageReceiveV1(ctx context.Context, event *larkim.P2MessageReceiveV1) error {
	msg, err := InboundMessageFromP2MessageReceiveV1(event)
	if err != nil {
		log.Printf("feishu long connection message ignored: %v", err)
		return nil
	}

	_, _ = HandleInboundMessage(ctx, r.Service, r.Notifier, msg)
	return nil
}

func (r WSReceiver) handleP2CardActionTrigger(ctx context.Context, event *callback.CardActionTriggerEvent) (*callback.CardActionTriggerResponse, error) {
	if event == nil || event.Event == nil || event.Event.Action == nil {
		return cardToast("RelayX error: missing card action"), nil
	}

	value := stringifyActionValue(event.Event.Action.Value)
	approvalID := value["approval_id"]
	decision := codex.ApprovalDecision(value["decision"])
	userID := ""
	if event.Event.Operator != nil {
		userID = firstNonEmpty(event.Event.Operator.OpenID, ptrString(event.Event.Operator.UserID))
	}
	if approvalID == "" || decision == "" {
		return cardToast("RelayX error: approval_id and decision are required"), nil
	}

	reply, err := r.Service.HandleApproval(ctx, userID, approvalID, decision)
	if err != nil {
		log.Printf("feishu long connection card action failed approval_id=%q user_id=%q: %v", approvalID, userID, err)
		return cardToast(fmt.Sprintf("RelayX error: %s", err)), nil
	}

	if r.Notifier != nil && reply.Text != "" && reply.Approval != nil {
		if task, ok := r.Service.TaskByID(reply.Approval.TaskID); ok {
			if err := r.Notifier.SendMessage(ctx, task.ChatID, reply.Text); err != nil {
				log.Printf("feishu send approval result failed chat_id=%q: %v", task.ChatID, err)
			}
		}
	}
	return cardToast(reply.Text), nil
}

func InboundMessageFromP2MessageReceiveV1(event *larkim.P2MessageReceiveV1) (app.InboundMessage, error) {
	if event == nil || event.Event == nil {
		return app.InboundMessage{}, fmt.Errorf("missing event")
	}
	if event.Event.Message == nil {
		return app.InboundMessage{}, fmt.Errorf("missing event.message")
	}
	if event.Event.Sender == nil || event.Event.Sender.SenderId == nil {
		return app.InboundMessage{}, fmt.Errorf("missing event.sender.sender_id")
	}

	text := extractMessageText(ptrString(event.Event.Message.Content))
	text = stripLeadingMentions(text, event.Event.Message.Mentions)
	return app.InboundMessage{
		ChatID: ptrString(event.Event.Message.ChatId),
		UserID: firstNonEmpty(
			ptrString(event.Event.Sender.SenderId.OpenId),
			ptrString(event.Event.Sender.SenderId.UserId),
		),
		Text: text,
	}, nil
}

func cardToast(text string) *callback.CardActionTriggerResponse {
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{
			Type:    "info",
			Content: core.RedactSecrets(text),
		},
	}
}

func stringifyActionValue(value map[string]interface{}) map[string]string {
	out := make(map[string]string, len(value))
	for key, val := range value {
		if val == nil {
			continue
		}
		if s, ok := val.(string); ok {
			out[key] = s
			continue
		}
		out[key] = fmt.Sprint(val)
	}
	return out
}

func stripLeadingMentions(text string, mentions []*larkim.MentionEvent) string {
	text = strings.TrimSpace(text)
	for {
		changed := false
		for _, mention := range mentions {
			if mention == nil || mention.Key == nil || *mention.Key == "" {
				continue
			}
			next := strings.TrimSpace(strings.TrimPrefix(text, *mention.Key))
			if next != text {
				text = next
				changed = true
			}
		}
		if !changed {
			return text
		}
	}
}

func feishuWSDomain(baseURL string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return "https://open.feishu.cn"
	}
	return strings.TrimSuffix(baseURL, "/open-apis")
}

func ptrString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
