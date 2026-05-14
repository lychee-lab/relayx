package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/lychee-lab/relayx/internal/app"
	"github.com/lychee-lab/relayx/internal/codex"
)

type CallbackHandler struct {
	Service           *app.Service
	Notifier          app.Notifier
	VerificationToken string
}

func (h CallbackHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var envelope map[string]any
	if err := json.NewDecoder(r.Body).Decode(&envelope); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	if envelope["type"] == "url_verification" {
		if !h.verifyToken(envelope) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "invalid verification token"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"challenge": envelope["challenge"]})
		return
	}

	if !h.verifyToken(envelope) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "invalid verification token"})
		return
	}

	eventType := nestedString(envelope, "header", "event_type")
	if eventType == "" {
		eventType = stringField(envelope, "type")
	}

	switch eventType {
	case "im.message.receive_v1":
		h.handleMessage(w, r.Context(), envelope)
	case "card.action.trigger":
		h.handleCardAction(w, r.Context(), envelope)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"code": 0, "msg": "ignored"})
	}
}

func (h CallbackHandler) handleMessage(w http.ResponseWriter, ctx context.Context, envelope map[string]any) {
	handled, err := h.recordEvent(ctx, messageEventIDsFromEnvelope(envelope)...)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"code": 1, "msg": err.Error()})
		return
	}
	if !handled {
		writeJSON(w, http.StatusOK, map[string]any{"code": 0, "msg": "duplicate"})
		return
	}

	msg := app.InboundMessage{
		ChatID: nestedString(envelope, "event", "message", "chat_id"),
		UserID: firstNonEmpty(
			nestedString(envelope, "event", "sender", "sender_id", "open_id"),
			nestedString(envelope, "event", "sender", "sender_id", "user_id"),
		),
		Text: extractMessageText(nestedString(envelope, "event", "message", "content")),
	}
	reply, err := HandleInboundMessage(ctx, h.Service, h.Notifier, msg)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"code": 1, "msg": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"code": 0, "msg": "ok", "reply": reply.Text})
}

func (h CallbackHandler) handleCardAction(w http.ResponseWriter, ctx context.Context, envelope map[string]any) {
	handled, err := h.recordEvent(ctx, cardActionEventIDsFromEnvelope(envelope)...)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"code": 1, "msg": err.Error()})
		return
	}
	if !handled {
		writeJSON(w, http.StatusOK, map[string]any{"code": 0, "msg": "duplicate", "toast": "duplicate event ignored"})
		return
	}

	action := nestedString(envelope, "event", "action", "value", "action")
	if action == "resume_thread" {
		h.handleResumeAction(w, ctx, envelope)
		return
	}

	approvalID := nestedString(envelope, "event", "action", "value", "approval_id")
	decision := codex.ApprovalDecision(nestedString(envelope, "event", "action", "value", "decision"))
	userID := firstNonEmpty(
		nestedString(envelope, "event", "operator", "operator_id", "open_id"),
		nestedString(envelope, "event", "operator", "operator_id", "user_id"),
	)
	if approvalID == "" || decision == "" {
		writeJSON(w, http.StatusOK, map[string]any{"code": 1, "msg": "approval_id and decision are required"})
		return
	}
	reply, err := h.Service.HandleApproval(ctx, userID, approvalID, decision)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"code": 1, "msg": err.Error()})
		return
	}
	if h.Notifier != nil && reply.Text != "" && reply.Approval != nil {
		if task, ok := h.Service.TaskByID(reply.Approval.TaskID); ok {
			if err := h.Notifier.SendMessage(ctx, task.ChatID, reply.Text); err != nil {
				log.Printf("feishu send approval result failed chat_id=%q: %v", task.ChatID, err)
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"code": 0, "msg": "ok", "toast": reply.Text})
}

func (h CallbackHandler) handleResumeAction(w http.ResponseWriter, ctx context.Context, envelope map[string]any) {
	threadID := nestedString(envelope, "event", "action", "value", "thread_id")
	chatID := nestedString(envelope, "event", "action", "value", "chat_id")
	cwd := nestedString(envelope, "event", "action", "value", "cwd")
	userID := firstNonEmpty(
		nestedString(envelope, "event", "operator", "operator_id", "open_id"),
		nestedString(envelope, "event", "operator", "operator_id", "user_id"),
	)
	if threadID == "" || chatID == "" {
		writeJSON(w, http.StatusOK, map[string]any{"code": 1, "msg": "thread_id and chat_id are required"})
		return
	}
	reply, err := h.Service.HandleResumeSelection(ctx, userID, chatID, threadID, cwd)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"code": 1, "msg": err.Error()})
		return
	}
	if h.Notifier != nil && reply.Text != "" {
		_ = h.Notifier.SendMessage(ctx, chatID, reply.Text)
	}
	writeJSON(w, http.StatusOK, map[string]any{"code": 0, "msg": "ok", "toast": reply.Text})
}

func (h CallbackHandler) recordEvent(ctx context.Context, eventIDs ...string) (bool, error) {
	if h.Service == nil {
		return true, nil
	}
	return h.Service.RecordExternalEvents(ctx, "feishu", eventIDs...)
}

func (h CallbackHandler) verifyToken(envelope map[string]any) bool {
	if h.VerificationToken == "" {
		return true
	}
	token := firstNonEmpty(
		stringField(envelope, "token"),
		nestedString(envelope, "header", "token"),
	)
	return token == h.VerificationToken
}

func messageEventIDsFromEnvelope(envelope map[string]any) []string {
	return prefixedEventIDs(
		prefixedEventID("event", nestedString(envelope, "header", "event_id")),
		prefixedEventID("message", nestedString(envelope, "event", "message", "message_id")),
	)
}

func cardActionEventIDsFromEnvelope(envelope map[string]any) []string {
	return prefixedEventIDs(prefixedEventID("event", nestedString(envelope, "header", "event_id")))
}

func prefixedEventID(prefix, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return prefix + ":" + value
}

func prefixedEventIDs(values ...string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func extractMessageText(content string) string {
	var value map[string]any
	if err := json.Unmarshal([]byte(content), &value); err != nil {
		return content
	}
	return stringField(value, "text")
}

func nestedString(m map[string]any, path ...string) string {
	var current any = m
	for _, key := range path {
		obj, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current = obj[key]
	}
	if s, ok := current.(string); ok {
		return s
	}
	return ""
}

func stringField(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
