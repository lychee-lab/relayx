package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

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
	msg := app.InboundMessage{
		ChatID: nestedString(envelope, "event", "message", "chat_id"),
		UserID: firstNonEmpty(
			nestedString(envelope, "event", "sender", "sender_id", "open_id"),
			nestedString(envelope, "event", "sender", "sender_id", "user_id"),
		),
		Text: extractMessageText(nestedString(envelope, "event", "message", "content")),
	}
	reply, err := h.Service.HandleMessage(ctx, msg)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"code": 1, "msg": err.Error()})
		return
	}
	if h.Notifier != nil && reply.Text != "" {
		_ = h.Notifier.SendMessage(ctx, msg.ChatID, reply.Text)
	}
	writeJSON(w, http.StatusOK, map[string]any{"code": 0, "msg": "ok", "reply": reply.Text})
}

func (h CallbackHandler) handleCardAction(w http.ResponseWriter, ctx context.Context, envelope map[string]any) {
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
			_ = h.Notifier.SendMessage(ctx, task.ChatID, reply.Text)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"code": 0, "msg": "ok", "toast": reply.Text})
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
