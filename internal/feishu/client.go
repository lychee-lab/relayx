package feishu

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/lychee-lab/relayx/internal/core"
)

type Client struct {
	AppID     string
	AppSecret string
	BaseURL   string
	HTTP      *http.Client

	mu          sync.Mutex
	tenantToken string
	tokenExpiry time.Time
}

type Notifier struct {
	Client *Client
}

func (n Notifier) SendMessage(ctx context.Context, chatID string, text string) error {
	if n.Client == nil {
		return fmt.Errorf("feishu client is nil")
	}
	return n.Client.SendMessage(ctx, Message{ChatID: chatID, Text: text})
}

func (n Notifier) SendMarkdown(ctx context.Context, chatID string, title string, markdown string) error {
	if n.Client == nil {
		return fmt.Errorf("feishu client is nil")
	}
	return n.Client.SendMarkdownCard(ctx, MarkdownCard{ChatID: chatID, Title: title, Markdown: markdown})
}

func (n Notifier) SendApproval(ctx context.Context, chatID string, approval core.Approval) error {
	if n.Client == nil {
		return fmt.Errorf("feishu client is nil")
	}
	return n.Client.SendApproval(ctx, chatID, approval)
}

func (n Notifier) SendResumeOptions(ctx context.Context, chatID string, options []core.ResumeOption) error {
	if n.Client == nil {
		return fmt.Errorf("feishu client is nil")
	}
	return n.Client.SendResumeOptions(ctx, chatID, options)
}

func (c *Client) SendMessage(ctx context.Context, msg Message) error {
	if msg.ChatID == "" {
		return fmt.Errorf("chat id is required")
	}
	content, err := json.Marshal(map[string]string{"text": core.RedactSecrets(msg.Text)})
	if err != nil {
		return err
	}
	return c.sendMessage(ctx, msg.ChatID, "text", string(content))
}

func (c *Client) SendApprovalCard(ctx context.Context, card ApprovalCard) error {
	if card.ChatID == "" {
		return fmt.Errorf("chat id is required")
	}
	payload := approvalCardPayload(card)
	content, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return c.sendMessage(ctx, card.ChatID, "interactive", string(content))
}

func (c *Client) SendResumeCard(ctx context.Context, card ResumeCard) error {
	if card.ChatID == "" {
		return fmt.Errorf("chat id is required")
	}
	payload := resumeCardPayload(card)
	content, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return c.sendMessage(ctx, card.ChatID, "interactive", string(content))
}

func (c *Client) SendMarkdownCard(ctx context.Context, card MarkdownCard) error {
	if card.ChatID == "" {
		return fmt.Errorf("chat id is required")
	}
	payload := markdownCardPayload(card)
	content, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return c.sendMessage(ctx, card.ChatID, "interactive", string(content))
}

func (c *Client) SendText(ctx context.Context, chatID string, text string) error {
	return c.SendMessage(ctx, Message{ChatID: chatID, Text: text})
}

func (c *Client) SendApproval(ctx context.Context, chatID string, approval core.Approval) error {
	return c.SendApprovalCard(ctx, ApprovalCard{
		ChatID:     chatID,
		ApprovalID: approval.ID,
		Title:      "Codex approval required",
		Summary:    approval.Summary,
		Actions:    []string{"approved", "approved_for_session", "denied", "abort"},
	})
}

func (c *Client) SendResumeOptions(ctx context.Context, chatID string, options []core.ResumeOption) error {
	return c.SendResumeCard(ctx, ResumeCard{
		ChatID:  chatID,
		Options: options,
	})
}

func (c *Client) sendMessage(ctx context.Context, chatID string, msgType string, content string) error {
	token, err := c.tenantAccessToken(ctx)
	if err != nil {
		return err
	}
	body := map[string]any{
		"receive_id": chatID,
		"msg_type":   msgType,
		"content":    content,
	}
	var out map[string]any
	return c.doJSON(ctx, http.MethodPost, "/im/v1/messages?receive_id_type=chat_id", token, body, &out)
}

func (c *Client) tenantAccessToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	if c.tenantToken != "" && time.Now().Before(c.tokenExpiry.Add(-2*time.Minute)) {
		token := c.tenantToken
		c.mu.Unlock()
		return token, nil
	}
	c.mu.Unlock()

	if c.AppID == "" || c.AppSecret == "" {
		return "", fmt.Errorf("feishu app id and secret are required")
	}

	var out struct {
		Code              int    `json:"code"`
		Msg               string `json:"msg"`
		TenantAccessToken string `json:"tenant_access_token"`
		Expire            int    `json:"expire"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/auth/v3/tenant_access_token/internal", "", map[string]string{
		"app_id":     c.AppID,
		"app_secret": c.AppSecret,
	}, &out); err != nil {
		return "", err
	}
	if out.Code != 0 {
		return "", fmt.Errorf("feishu token error %d: %s", out.Code, out.Msg)
	}
	if out.TenantAccessToken == "" {
		return "", fmt.Errorf("feishu token response missing tenant_access_token")
	}

	c.mu.Lock()
	c.tenantToken = out.TenantAccessToken
	c.tokenExpiry = time.Now().Add(time.Duration(out.Expire) * time.Second)
	c.mu.Unlock()

	return out.TenantAccessToken, nil
}

func (c *Client) doJSON(ctx context.Context, method, path, bearer string, input any, output any) error {
	var body bytes.Buffer
	if input != nil {
		if err := json.NewEncoder(&body).Encode(input); err != nil {
			return err
		}
	}

	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.baseURL(), "/")+path, &body)
	if err != nil {
		return err
	}
	req.Header.Set("content-type", "application/json")
	if bearer != "" {
		req.Header.Set("authorization", "Bearer "+bearer)
	}

	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("feishu http %s %s returned %s", method, path, resp.Status)
	}
	if output == nil {
		return nil
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	var envelope struct {
		Code int             `json:"code"`
		Msg  string          `json:"msg"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return err
	}
	if envelope.Code != 0 {
		return fmt.Errorf("feishu api error %d: %s", envelope.Code, envelope.Msg)
	}
	if len(envelope.Data) > 0 {
		if err := json.Unmarshal(envelope.Data, output); err == nil {
			return nil
		}
	}
	if err := json.Unmarshal(raw, output); err == nil {
		return nil
	}
	if len(envelope.Data) == 0 {
		return json.Unmarshal(raw, output)
	}
	return json.Unmarshal(envelope.Data, output)
}

func (c *Client) baseURL() string {
	if c.BaseURL == "" {
		return "https://open.feishu.cn/open-apis"
	}
	return c.BaseURL
}

func approvalCardPayload(card ApprovalCard) map[string]any {
	elements := []map[string]any{
		{
			"tag":     "div",
			"content": fmt.Sprintf("**%s**", core.RedactSecrets(card.Summary)),
		},
	}
	for _, action := range card.Actions {
		elements = append(elements, map[string]any{
			"tag": "button",
			"text": map[string]any{
				"tag":     "plain_text",
				"content": actionLabel(action),
			},
			"type":  buttonType(action),
			"value": map[string]string{"approval_id": card.ApprovalID, "decision": action},
		})
	}
	return map[string]any{
		"config": map[string]any{"wide_screen_mode": true},
		"header": map[string]any{
			"title": map[string]any{"tag": "plain_text", "content": card.Title},
		},
		"elements": elements,
	}
}

func resumeCardPayload(card ResumeCard) map[string]any {
	elements := []map[string]any{
		{
			"tag":     "div",
			"content": "**选择要恢复的 Codex session**",
		},
	}
	limit := len(card.Options)
	if limit > 10 {
		limit = 10
	}
	for i := 0; i < limit; i++ {
		option := card.Options[i]
		title := firstNonEmpty(option.Title, option.Preview, option.ThreadID)
		summary := fmt.Sprintf("**%s**\n%s", core.RedactSecrets(truncate(title, 80)), core.RedactSecrets(option.CWD))
		elements = append(elements,
			map[string]any{
				"tag":     "div",
				"content": summary,
			},
			map[string]any{
				"tag": "button",
				"text": map[string]any{
					"tag":     "plain_text",
					"content": fmt.Sprintf("恢复 %d", i+1),
				},
				"type": "primary",
				"value": map[string]string{
					"action":    "resume_thread",
					"chat_id":   card.ChatID,
					"thread_id": option.ThreadID,
					"cwd":       option.CWD,
				},
			},
		)
	}
	return map[string]any{
		"config": map[string]any{"wide_screen_mode": true},
		"header": map[string]any{
			"title": map[string]any{"tag": "plain_text", "content": "Codex resume"},
		},
		"elements": elements,
	}
}

func markdownCardPayload(card MarkdownCard) map[string]any {
	title := strings.TrimSpace(card.Title)
	if title == "" {
		title = "Codex result"
	}
	markdown := core.RedactSecrets(strings.TrimSpace(card.Markdown))
	if markdown == "" {
		markdown = "_No content._"
	}
	return map[string]any{
		"config": map[string]any{"wide_screen_mode": true},
		"header": map[string]any{
			"title": map[string]any{"tag": "plain_text", "content": title},
		},
		"elements": []map[string]any{
			{
				"tag":     "markdown",
				"content": markdown,
			},
		},
	}
}

func actionLabel(action string) string {
	switch action {
	case "approved":
		return "批准一次"
	case "approved_for_session":
		return "本轮批准"
	case "denied":
		return "拒绝"
	case "abort":
		return "终止任务"
	default:
		return action
	}
}

func buttonType(action string) string {
	switch action {
	case "denied", "abort":
		return "danger"
	default:
		return "primary"
	}
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
