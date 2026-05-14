package feishu

import (
	"context"
	"strings"
	"testing"

	larkevent "github.com/larksuite/oapi-sdk-go/v3/event"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	"github.com/lychee-lab/relayx/internal/app"
	"github.com/lychee-lab/relayx/internal/core"
)

func TestInboundMessageFromP2MessageReceiveV1(t *testing.T) {
	chatID := "oc_1"
	openID := "ou_1"
	content := `{"text":"@_user_1 /codex help"}`
	mentionKey := "@_user_1"

	msg, err := InboundMessageFromP2MessageReceiveV1(&larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Sender: &larkim.EventSender{
				SenderId: &larkim.UserId{OpenId: &openID},
			},
			Message: &larkim.EventMessage{
				ChatId:   &chatID,
				Content:  &content,
				Mentions: []*larkim.MentionEvent{{Key: &mentionKey}},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if msg.ChatID != "oc_1" || msg.UserID != "ou_1" || msg.Text != "/codex help" {
		t.Fatalf("message = %#v", msg)
	}
}

func TestWSReceiverHandlesP2MessageReceiveV1(t *testing.T) {
	chatID := "oc_1"
	openID := "ou_1"
	content := `{"text":"/codex help"}`
	notifier := &callbackNotifier{}
	receiver := WSReceiver{
		Service:  app.NewService(core.NewTaskManager()),
		Notifier: notifier,
	}

	err := receiver.handleP2MessageReceiveV1(context.Background(), &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Sender: &larkim.EventSender{
				SenderId: &larkim.UserId{OpenId: &openID},
			},
			Message: &larkim.EventMessage{
				ChatId:  &chatID,
				Content: &content,
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(notifier.messages) != 1 || !strings.Contains(notifier.messages[0], "Usage:") {
		t.Fatalf("messages = %#v", notifier.messages)
	}
}

func TestWSReceiverDeduplicatesP2MessageReceiveV1(t *testing.T) {
	chatID := "oc_1"
	openID := "ou_1"
	messageID := "om_1"
	content := `{"text":"/codex help"}`
	notifier := &callbackNotifier{}
	receiver := WSReceiver{
		Service:  app.NewService(core.NewTaskManager()),
		Notifier: notifier,
	}
	event := &larkim.P2MessageReceiveV1{
		EventV2Base: &larkevent.EventV2Base{
			Header: &larkevent.EventHeader{EventID: "evt_1"},
		},
		Event: &larkim.P2MessageReceiveV1Data{
			Sender: &larkim.EventSender{
				SenderId: &larkim.UserId{OpenId: &openID},
			},
			Message: &larkim.EventMessage{
				MessageId: &messageID,
				ChatId:    &chatID,
				Content:   &content,
			},
		},
	}

	if err := receiver.handleP2MessageReceiveV1(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if err := receiver.handleP2MessageReceiveV1(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if len(notifier.messages) != 1 {
		t.Fatalf("messages = %#v", notifier.messages)
	}
}

func TestFeishuWSDomain(t *testing.T) {
	cases := map[string]string{
		"":                                     "https://open.feishu.cn",
		"https://open.feishu.cn/open-apis":     "https://open.feishu.cn",
		"https://open.feishu.cn/open-apis/":    "https://open.feishu.cn",
		"https://example.test/custom-open-api": "https://example.test/custom-open-api",
	}

	for input, want := range cases {
		if got := feishuWSDomain(input); got != want {
			t.Fatalf("feishuWSDomain(%q) = %q, want %q", input, got, want)
		}
	}
}
