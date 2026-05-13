package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"sync"
	"sync/atomic"
)

type JSONRPCAdapter struct {
	rwc       io.ReadWriteCloser
	encMu     sync.Mutex
	pendingMu sync.Mutex
	pending   map[string]chan rpcResponse
	approval  map[string]serverApproval
	nextID    atomic.Int64
	events    chan Event
	approvals chan ApprovalRequest
	closed    chan struct{}
	closeOnce sync.Once
}

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc,omitempty"`
	ID      any    `json:"id,omitempty"`
	Method  string `json:"method,omitempty"`
	Params  any    `json:"params,omitempty"`
}

type rpcResponse struct {
	ID     any             `json:"id,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type serverApproval struct {
	RequestID any
	Method    string
	Kind      string
}

func NewJSONRPCAdapter(rwc io.ReadWriteCloser) *JSONRPCAdapter {
	a := &JSONRPCAdapter{
		rwc:       rwc,
		pending:   make(map[string]chan rpcResponse),
		approval:  make(map[string]serverApproval),
		events:    make(chan Event, 64),
		approvals: make(chan ApprovalRequest, 64),
		closed:    make(chan struct{}),
	}
	go a.readLoop()
	return a
}

func (a *JSONRPCAdapter) Initialize(ctx context.Context) error {
	var out map[string]any
	return a.call(ctx, "initialize", map[string]any{
		"clientInfo": map[string]any{
			"name":    "relayx",
			"title":   "RelayX",
			"version": "0.1.0",
		},
		"capabilities": map[string]any{
			"experimentalApi": true,
		},
	}, &out)
}

func (a *JSONRPCAdapter) StartThread(ctx context.Context, req StartThreadRequest) (ThreadID, error) {
	params := map[string]any{
		"cwd":            req.CWD,
		"sandbox":        defaultString(req.Sandbox, "workspace-write"),
		"approvalPolicy": defaultString(req.ApprovalPolicy, "on-request"),
	}
	if req.Model != "" {
		params["model"] = req.Model
	}

	var out map[string]any
	if err := a.call(ctx, "thread/start", params, &out); err != nil {
		return "", err
	}

	id := extractString(out, "thread", "id")
	if id == "" {
		id = extractString(out, "threadId")
	}
	if id == "" {
		return "", fmt.Errorf("thread/start response missing thread id")
	}
	return ThreadID(id), nil
}

func (a *JSONRPCAdapter) StartTurn(ctx context.Context, req StartTurnRequest) (TurnID, error) {
	params := map[string]any{
		"threadId": string(req.ThreadID),
		"input": []map[string]any{
			{"type": "text", "text": req.Text},
		},
	}
	if req.Model != "" {
		params["model"] = req.Model
	}
	if req.Effort != "" {
		params["effort"] = req.Effort
	}

	var out map[string]any
	if err := a.call(ctx, "turn/start", params, &out); err != nil {
		return "", err
	}

	id := extractString(out, "turn", "id")
	if id == "" {
		id = extractString(out, "turnId")
	}
	if id == "" {
		return "", fmt.Errorf("turn/start response missing turn id")
	}
	return TurnID(id), nil
}

func (a *JSONRPCAdapter) StartReview(ctx context.Context, req StartReviewRequest) (StartReviewResponse, error) {
	params := map[string]any{
		"threadId": string(req.ThreadID),
		"target":   reviewTargetParam(req.Target),
	}
	if req.Delivery != "" {
		params["delivery"] = req.Delivery
	}

	var out map[string]any
	if err := a.call(ctx, "review/start", params, &out); err != nil {
		return StartReviewResponse{}, err
	}

	resp := StartReviewResponse{
		ReviewThreadID: ThreadID(extractString(out, "reviewThreadId")),
		TurnID:         TurnID(extractString(out, "turn", "id")),
	}
	return resp, nil
}

func (a *JSONRPCAdapter) ListModels(ctx context.Context) ([]ModelInfo, error) {
	var out struct {
		Data []struct {
			ID                     string `json:"id"`
			Model                  string `json:"model"`
			DisplayName            string `json:"displayName"`
			DefaultReasoningEffort string `json:"defaultReasoningEffort"`
			Hidden                 bool   `json:"hidden"`
			SupportedEfforts       []struct {
				ReasoningEffort string `json:"reasoningEffort"`
			} `json:"supportedReasoningEfforts"`
		} `json:"data"`
	}
	if err := a.call(ctx, "model/list", map[string]any{"includeHidden": false}, &out); err != nil {
		return nil, err
	}

	models := make([]ModelInfo, 0, len(out.Data))
	for _, item := range out.Data {
		model := ModelInfo{
			ID:                     item.ID,
			Model:                  item.Model,
			DisplayName:            item.DisplayName,
			DefaultReasoningEffort: item.DefaultReasoningEffort,
			Hidden:                 item.Hidden,
			SupportedEfforts:       make([]string, 0, len(item.SupportedEfforts)),
		}
		for _, effort := range item.SupportedEfforts {
			if effort.ReasoningEffort != "" {
				model.SupportedEfforts = append(model.SupportedEfforts, effort.ReasoningEffort)
			}
		}
		models = append(models, model)
	}
	return models, nil
}

func (a *JSONRPCAdapter) ListThreads(ctx context.Context, req ListThreadsRequest) ([]ThreadInfo, error) {
	limit := req.Limit
	if limit <= 0 {
		limit = 5
	}
	params := map[string]any{
		"limit":         limit,
		"sortKey":       "updated_at",
		"sortDirection": "desc",
		"archived":      false,
	}
	if req.CWD != "" {
		params["cwd"] = req.CWD
	}

	var out struct {
		Data []threadDTO `json:"data"`
	}
	if err := a.call(ctx, "thread/list", params, &out); err != nil {
		return nil, err
	}

	threads := make([]ThreadInfo, 0, len(out.Data))
	for _, thread := range out.Data {
		threads = append(threads, thread.toInfo())
	}
	return threads, nil
}

func (a *JSONRPCAdapter) ResumeThread(ctx context.Context, req ResumeThreadRequest) (ResumeThreadResponse, error) {
	params := map[string]any{
		"threadId": string(req.ThreadID),
		"sandbox":  defaultString(req.Sandbox, "workspace-write"),
	}
	if req.Model != "" {
		params["model"] = req.Model
	}
	if req.Effort != "" {
		params["config"] = map[string]any{"model_reasoning_effort": req.Effort}
	}
	if req.ApprovalPolicy != "" {
		params["approvalPolicy"] = req.ApprovalPolicy
	}

	var out struct {
		Thread        threadDTO `json:"thread"`
		Model         string    `json:"model"`
		ModelProvider string    `json:"modelProvider"`
		CWD           string    `json:"cwd"`
		Effort        string    `json:"reasoningEffort"`
	}
	if err := a.call(ctx, "thread/resume", params, &out); err != nil {
		return ResumeThreadResponse{}, err
	}

	resp := ResumeThreadResponse{
		Thread:        out.Thread.toInfo(),
		Model:         out.Model,
		ModelProvider: out.ModelProvider,
		CWD:           firstNonEmpty(out.CWD, out.Thread.CWD),
		Effort:        out.Effort,
	}
	return resp, nil
}

func (a *JSONRPCAdapter) SteerTurn(ctx context.Context, threadID ThreadID, turnID TurnID, text string) error {
	var out map[string]any
	return a.call(ctx, "turn/steer", map[string]any{
		"threadId":       string(threadID),
		"expectedTurnId": string(turnID),
		"input": []map[string]any{
			{"type": "text", "text": text},
		},
	}, &out)
}

func (a *JSONRPCAdapter) RespondApproval(ctx context.Context, approvalID string, decision ApprovalDecision) error {
	a.pendingMu.Lock()
	pending, ok := a.approval[approvalID]
	if ok {
		delete(a.approval, approvalID)
	}
	a.pendingMu.Unlock()
	if !ok {
		return fmt.Errorf("approval %q not found", approvalID)
	}

	return a.write(ctx, rpcResponse{
		ID:     pending.RequestID,
		Result: mustRawJSON(approvalResult(pending.Method, decision)),
	})
}

func (a *JSONRPCAdapter) Events() <-chan Event {
	return a.events
}

func (a *JSONRPCAdapter) Approvals() <-chan ApprovalRequest {
	return a.approvals
}

func (a *JSONRPCAdapter) Close() error {
	var err error
	a.closeOnce.Do(func() {
		close(a.closed)
		err = a.rwc.Close()
	})
	return err
}

func (a *JSONRPCAdapter) call(ctx context.Context, method string, params any, out any) error {
	id := strconv.FormatInt(a.nextID.Add(1), 10)
	ch := make(chan rpcResponse, 1)

	a.pendingMu.Lock()
	a.pending[id] = ch
	a.pendingMu.Unlock()

	if err := a.write(ctx, rpcRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}); err != nil {
		a.pendingMu.Lock()
		delete(a.pending, id)
		a.pendingMu.Unlock()
		return err
	}

	select {
	case <-ctx.Done():
		a.pendingMu.Lock()
		delete(a.pending, id)
		a.pendingMu.Unlock()
		return ctx.Err()
	case <-a.closed:
		return io.ErrClosedPipe
	case resp := <-ch:
		if resp.Error != nil {
			return fmt.Errorf("codex rpc %s failed: %s", method, resp.Error.Message)
		}
		if out == nil || len(resp.Result) == 0 {
			return nil
		}
		return json.Unmarshal(resp.Result, out)
	}
}

func (a *JSONRPCAdapter) write(ctx context.Context, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	data = append(data, '\n')

	done := make(chan error, 1)
	go func() {
		a.encMu.Lock()
		defer a.encMu.Unlock()
		_, err := a.rwc.Write(data)
		done <- err
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-done:
		return err
	}
}

func (a *JSONRPCAdapter) readLoop() {
	defer close(a.events)
	defer close(a.approvals)
	defer a.Close()

	scanner := bufio.NewScanner(a.rwc)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	for scanner.Scan() {
		var envelope map[string]json.RawMessage
		if err := json.Unmarshal(scanner.Bytes(), &envelope); err != nil {
			a.emitEvent(Event{Kind: "protocol/error", Message: err.Error()})
			continue
		}

		if _, ok := envelope["method"]; ok {
			a.handleRequestOrNotification(envelope)
			continue
		}
		if _, ok := envelope["id"]; ok {
			a.handleResponse(envelope)
		}
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, io.ErrClosedPipe) {
		a.emitEvent(Event{Kind: "protocol/closed", Message: err.Error()})
	}
}

func (a *JSONRPCAdapter) handleResponse(envelope map[string]json.RawMessage) {
	var resp rpcResponse
	if err := decodeEnvelope(envelope, &resp); err != nil {
		a.emitEvent(Event{Kind: "protocol/error", Message: err.Error()})
		return
	}
	id := idKey(resp.ID)

	a.pendingMu.Lock()
	ch, ok := a.pending[id]
	if ok {
		delete(a.pending, id)
	}
	a.pendingMu.Unlock()

	if ok {
		ch <- resp
	}
}

func (a *JSONRPCAdapter) handleRequestOrNotification(envelope map[string]json.RawMessage) {
	var method string
	_ = json.Unmarshal(envelope["method"], &method)

	var params map[string]any
	if raw := envelope["params"]; len(raw) > 0 {
		_ = json.Unmarshal(raw, &params)
	}

	if isApprovalMethod(method) {
		var requestID any
		_ = json.Unmarshal(envelope["id"], &requestID)
		approval := approvalFromParams(method, requestID, params)

		a.pendingMu.Lock()
		a.approval[approval.ID] = serverApproval{
			RequestID: requestID,
			Method:    method,
			Kind:      approval.Kind,
		}
		a.pendingMu.Unlock()

		select {
		case a.approvals <- approval:
		case <-a.closed:
		}
		return
	}

	a.emitEvent(eventFromNotification(method, params))
}

func (a *JSONRPCAdapter) emitEvent(event Event) {
	select {
	case a.events <- event:
	case <-a.closed:
	}
}

func decodeEnvelope(envelope map[string]json.RawMessage, out any) error {
	data, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

func isApprovalMethod(method string) bool {
	switch method {
	case "item/commandExecution/requestApproval", "item/fileChange/requestApproval", "item/permissions/requestApproval", "execCommandApproval", "applyPatchApproval":
		return true
	default:
		return false
	}
}

func approvalFromParams(method string, requestID any, params map[string]any) ApprovalRequest {
	approvalID := stringValue(params, "approvalId")
	if approvalID == "" {
		approvalID = stringValue(params, "itemId")
	}
	if approvalID == "" {
		approvalID = stringValue(params, "callId")
	}
	if approvalID == "" {
		approvalID = idKey(requestID)
	}

	threadID := stringValue(params, "threadId")
	if threadID == "" {
		threadID = stringValue(params, "conversationId")
	}

	summary := stringValue(params, "command")
	if summary == "" {
		summary = stringValue(params, "reason")
	}
	if summary == "" {
		summary = method
	}

	return ApprovalRequest{
		ID:       approvalID,
		ThreadID: ThreadID(threadID),
		TurnID:   TurnID(stringValue(params, "turnId")),
		Kind:     method,
		Summary:  summary,
		Payload:  params,
	}
}

func eventFromNotification(method string, params map[string]any) Event {
	event := Event{
		Kind:    method,
		Message: method,
		Payload: params,
	}
	if threadID := stringValue(params, "threadId"); threadID != "" {
		event.ThreadID = ThreadID(threadID)
	}
	if turnID := stringValue(params, "turnId"); turnID != "" {
		event.TurnID = TurnID(turnID)
	}
	if event.ThreadID == "" {
		if thread, ok := params["thread"].(map[string]any); ok {
			event.ThreadID = ThreadID(stringValue(thread, "id"))
		}
	}
	if event.TurnID == "" {
		if turn, ok := params["turn"].(map[string]any); ok {
			event.TurnID = TurnID(stringValue(turn, "id"))
		}
	}
	if msg := stringValue(params, "message"); msg != "" {
		event.Message = msg
	}
	return event
}

func approvalResult(method string, decision ApprovalDecision) any {
	switch method {
	case "item/commandExecution/requestApproval":
		return map[string]any{"decision": commandExecutionDecision(decision)}
	case "item/fileChange/requestApproval", "applyPatchApproval":
		return map[string]any{"decision": fileChangeDecision(decision)}
	case "execCommandApproval":
		return map[string]any{"decision": legacyDecision(decision)}
	case "item/permissions/requestApproval":
		return map[string]any{"permissions": map[string]any{}, "scope": "turn"}
	default:
		return map[string]any{"decision": string(decision)}
	}
}

func commandExecutionDecision(decision ApprovalDecision) string {
	switch decision {
	case ApprovalApproved:
		return "accept"
	case ApprovalApprovedForTurn:
		return "acceptForSession"
	case ApprovalAbort:
		return "cancel"
	default:
		return "decline"
	}
}

func fileChangeDecision(decision ApprovalDecision) string {
	switch decision {
	case ApprovalApproved:
		return "accept"
	case ApprovalApprovedForTurn:
		return "acceptForSession"
	case ApprovalAbort:
		return "cancel"
	default:
		return "decline"
	}
}

func legacyDecision(decision ApprovalDecision) string {
	switch decision {
	case ApprovalApproved:
		return "approved"
	case ApprovalApprovedForTurn:
		return "approved_for_session"
	case ApprovalAbort:
		return "abort"
	default:
		return "denied"
	}
}

func mustRawJSON(value any) json.RawMessage {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return data
}

func extractString(m map[string]any, path ...string) string {
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

func stringValue(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	switch typed := v.(type) {
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	default:
		return fmt.Sprint(typed)
	}
}

func idKey(id any) string {
	switch typed := id.(type) {
	case string:
		return typed
	case json.Number:
		return typed.String()
	case float64:
		return strconv.FormatInt(int64(typed), 10)
	case int64:
		return strconv.FormatInt(typed, 10)
	case int:
		return strconv.Itoa(typed)
	default:
		return fmt.Sprint(typed)
	}
}

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func reviewTargetParam(target ReviewTarget) map[string]any {
	switch target.Type {
	case "baseBranch":
		return map[string]any{"type": "baseBranch", "branch": target.Branch}
	case "commit":
		return map[string]any{"type": "commit", "sha": target.CommitSHA, "title": nil}
	case "custom":
		return map[string]any{"type": "custom", "instructions": target.Instructions}
	default:
		return map[string]any{"type": "uncommittedChanges"}
	}
}

type threadDTO struct {
	ID            string `json:"id"`
	SessionID     string `json:"sessionId"`
	Name          string `json:"name"`
	Preview       string `json:"preview"`
	CWD           string `json:"cwd"`
	ModelProvider string `json:"modelProvider"`
	UpdatedAt     int64  `json:"updatedAt"`
}

func (t threadDTO) toInfo() ThreadInfo {
	return ThreadInfo{
		ID:            t.ID,
		SessionID:     t.SessionID,
		Name:          t.Name,
		Preview:       t.Preview,
		CWD:           t.CWD,
		ModelProvider: t.ModelProvider,
		UpdatedAt:     t.UpdatedAt,
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
