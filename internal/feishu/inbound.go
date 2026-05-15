package feishu

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/lychee-lab/relayx/internal/app"
	"github.com/lychee-lab/relayx/internal/core"
)

func HandleInboundMessage(ctx context.Context, service *app.Service, notifier app.Notifier, msg app.InboundMessage) (app.Reply, error) {
	if service == nil {
		return app.Reply{}, fmt.Errorf("service is required")
	}
	log.Printf("feishu message received chat_id=%q user_id=%q text=%q", msg.ChatID, msg.UserID, core.RedactSecrets(msg.Text))

	reply, err := service.HandleMessage(ctx, msg)
	if err != nil {
		log.Printf("feishu message handle error chat_id=%q user_id=%q: %v", msg.ChatID, msg.UserID, err)
		sendMessageError(ctx, notifier, msg, err)
		return app.Reply{}, err
	}

	if notifier != nil && reply.Text != "" {
		if err := notifier.SendMessage(ctx, msg.ChatID, reply.Text); err != nil {
			log.Printf("feishu send message failed chat_id=%q: %v", msg.ChatID, err)
		}
	}
	return reply, nil
}

func sendMessageError(ctx context.Context, notifier app.Notifier, msg app.InboundMessage, err error) {
	if notifier == nil || msg.ChatID == "" {
		return
	}

	text := fmt.Sprintf("RelayX error: %s", err)
	if !isRelayXCommand(msg.Text) && core.IsUnsupportedCommand(err) {
		text = "RelayX handles /codex commands and shortcuts such as /model, /fast, /review, and /resume. Send /codex help for usage."
	}
	if sendErr := notifier.SendMessage(ctx, msg.ChatID, text); sendErr != nil {
		log.Printf("feishu send error message failed chat_id=%q: %v", msg.ChatID, sendErr)
	}
}

func isRelayXCommand(text string) bool {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) == 0 {
		return false
	}

	switch fields[0] {
	case "/codex", "/model", "/fast", "/review", "/resume":
		return true
	default:
		return false
	}
}
