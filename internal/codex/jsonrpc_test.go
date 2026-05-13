package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"
)

func TestJSONRPCAdapterThreadTurnAndApproval(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	adapter := NewJSONRPCAdapter(client)
	defer adapter.Close()

	done := make(chan map[string]any, 1)
	go fakeCodexServer(t, server, done)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	threadID, err := adapter.StartThread(ctx, StartThreadRequest{CWD: "/tmp/demo"})
	if err != nil {
		t.Fatal(err)
	}
	if threadID != "thread-1" {
		t.Fatalf("thread id = %q", threadID)
	}

	turnID, err := adapter.StartTurn(ctx, StartTurnRequest{ThreadID: threadID, Text: "fix bug"})
	if err != nil {
		t.Fatal(err)
	}
	if turnID != "turn-1" {
		t.Fatalf("turn id = %q", turnID)
	}

	approval := <-adapter.Approvals()
	if approval.ID != "approval-1" {
		t.Fatalf("approval id = %q", approval.ID)
	}
	if approval.ThreadID != "thread-1" || approval.TurnID != "turn-1" {
		t.Fatalf("approval = %#v", approval)
	}

	if err := adapter.RespondApproval(ctx, approval.ID, ApprovalApproved); err != nil {
		t.Fatal(err)
	}

	response := <-done
	result := response["result"].(map[string]any)
	if result["decision"] != "accept" {
		t.Fatalf("approval response = %#v", response)
	}
}

func TestJSONRPCAdapterModelListAndReview(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	adapter := NewJSONRPCAdapter(client)
	defer adapter.Close()

	done := make(chan map[string]any, 1)
	go fakeCodexModelReviewServer(t, server, done)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	models, err := adapter.ListModels(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0].Model != "gpt-5.2" || len(models[0].SupportedEfforts) != 1 || models[0].SupportedEfforts[0] != "high" {
		t.Fatalf("models = %#v", models)
	}

	resp, err := adapter.StartReview(ctx, StartReviewRequest{
		ThreadID: "thread-1",
		Target:   ReviewTarget{Type: "baseBranch", Branch: "main"},
		Delivery: "detached",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.ReviewThreadID != "review-thread-1" || resp.TurnID != "review-turn-1" {
		t.Fatalf("review response = %#v", resp)
	}

	req := <-done
	params := req["params"].(map[string]any)
	target := params["target"].(map[string]any)
	if req["method"] != "review/start" || params["delivery"] != "detached" || target["type"] != "baseBranch" || target["branch"] != "main" {
		t.Fatalf("review request = %#v", req)
	}
}

func TestJSONRPCAdapterThreadListAndResume(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	adapter := NewJSONRPCAdapter(client)
	defer adapter.Close()

	done := make(chan map[string]any, 1)
	go fakeCodexThreadListResumeServer(t, server, done)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	threads, err := adapter.ListThreads(ctx, ListThreadsRequest{CWD: "/tmp/demo", Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(threads) != 1 || threads[0].ID != "thread-1" || threads[0].CWD != "/tmp/demo" {
		t.Fatalf("threads = %#v", threads)
	}

	resp, err := adapter.ResumeThread(ctx, ResumeThreadRequest{
		ThreadID:       "thread-1",
		Model:          "gpt-5.2",
		Effort:         "high",
		Sandbox:        "workspace-write",
		ApprovalPolicy: "on-request",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Thread.ID != "thread-1" || resp.Model != "gpt-5.2" || resp.CWD != "/tmp/demo" {
		t.Fatalf("resume response = %#v", resp)
	}

	req := <-done
	params := req["params"].(map[string]any)
	config := params["config"].(map[string]any)
	if req["method"] != "thread/resume" || params["threadId"] != "thread-1" || params["model"] != "gpt-5.2" || config["model_reasoning_effort"] != "high" {
		t.Fatalf("resume request = %#v", req)
	}
}

func TestEventFromTurnCompletedExtractsLastAgentMessage(t *testing.T) {
	event := eventFromNotification("turn/completed", map[string]any{
		"threadId": "thread-1",
		"turn": map[string]any{
			"id": "turn-1",
			"items": []any{
				map[string]any{"type": "agentMessage", "text": "intermediate"},
				map[string]any{"type": "commandExecution", "command": "go test ./..."},
				map[string]any{"type": "agentMessage", "text": "final answer"},
			},
		},
	})

	if event.ThreadID != "thread-1" || event.TurnID != "turn-1" {
		t.Fatalf("event ids = %#v", event)
	}
	if event.Message != "final answer" {
		t.Fatalf("message = %q", event.Message)
	}
}

func TestEventFromAgentMessageDeltaExtractsDelta(t *testing.T) {
	event := eventFromNotification("item/agentMessage/delta", map[string]any{
		"threadId": "thread-1",
		"turnId":   "turn-1",
		"itemId":   "item-1",
		"delta":    "partial answer",
	})

	if event.ThreadID != "thread-1" || event.TurnID != "turn-1" {
		t.Fatalf("event ids = %#v", event)
	}
	if event.Message != "partial answer" {
		t.Fatalf("message = %q", event.Message)
	}
}

func fakeCodexServer(t *testing.T, conn net.Conn, done chan<- map[string]any) {
	t.Helper()
	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		var req map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		if _, ok := req["method"]; !ok {
			done <- req
			return
		}
		switch req["method"] {
		case "thread/start":
			writeRPC(t, conn, map[string]any{
				"id":     req["id"],
				"result": map[string]any{"thread": map[string]any{"id": "thread-1"}},
			})
		case "turn/start":
			writeRPC(t, conn, map[string]any{
				"id":     req["id"],
				"result": map[string]any{"turn": map[string]any{"id": "turn-1"}},
			})
			writeRPC(t, conn, map[string]any{
				"id":     "server-approval-1",
				"method": "item/commandExecution/requestApproval",
				"params": map[string]any{
					"itemId":   "approval-1",
					"threadId": "thread-1",
					"turnId":   "turn-1",
					"command":  "npm test",
				},
			})
		default:
			t.Errorf("unexpected method %v", req["method"])
			return
		}
	}
}

func fakeCodexModelReviewServer(t *testing.T, conn net.Conn, done chan<- map[string]any) {
	t.Helper()
	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		var req map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		switch req["method"] {
		case "model/list":
			writeRPC(t, conn, map[string]any{
				"id": req["id"],
				"result": map[string]any{
					"data": []map[string]any{
						{
							"id":                     "gpt-5.2",
							"model":                  "gpt-5.2",
							"displayName":            "GPT-5.2",
							"defaultReasoningEffort": "medium",
							"supportedReasoningEfforts": []map[string]any{
								{"reasoningEffort": "high", "description": "High"},
							},
						},
					},
				},
			})
		case "review/start":
			done <- req
			writeRPC(t, conn, map[string]any{
				"id": req["id"],
				"result": map[string]any{
					"reviewThreadId": "review-thread-1",
					"turn":           map[string]any{"id": "review-turn-1"},
				},
			})
			return
		default:
			t.Errorf("unexpected method %v", req["method"])
			return
		}
	}
}

func fakeCodexThreadListResumeServer(t *testing.T, conn net.Conn, done chan<- map[string]any) {
	t.Helper()
	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		var req map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		switch req["method"] {
		case "thread/list":
			writeRPC(t, conn, map[string]any{
				"id": req["id"],
				"result": map[string]any{
					"data": []map[string]any{
						{
							"id":            "thread-1",
							"sessionId":     "session-1",
							"name":          "Fix bug",
							"preview":       "fix bug",
							"cwd":           "/tmp/demo",
							"modelProvider": "openai",
							"updatedAt":     123,
						},
					},
				},
			})
		case "thread/resume":
			done <- req
			writeRPC(t, conn, map[string]any{
				"id": req["id"],
				"result": map[string]any{
					"thread": map[string]any{
						"id":            "thread-1",
						"sessionId":     "session-1",
						"name":          "Fix bug",
						"preview":       "fix bug",
						"cwd":           "/tmp/demo",
						"modelProvider": "openai",
						"updatedAt":     123,
					},
					"model":           "gpt-5.2",
					"modelProvider":   "openai",
					"cwd":             "/tmp/demo",
					"reasoningEffort": "high",
				},
			})
			return
		default:
			t.Errorf("unexpected method %v", req["method"])
			return
		}
	}
}

func writeRPC(t *testing.T, conn net.Conn, value map[string]any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	if _, err := conn.Write(data); err != nil {
		t.Fatal(err)
	}
}
