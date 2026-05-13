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
