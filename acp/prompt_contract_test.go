package acp

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"nekocode/protocol"
	rt "nekocode/runtime"
)

func TestPromptRealRuntimeDiscardsRetriedText(t *testing.T) {
	backend := newPromptContractBackend(func(_ context.Context, _ string, host rt.RunHost) (string, error) {
		host.Text("discarded-attempt")
		host.(rt.RunTextResetter).ResetText()
		host.Text("provisional-answer")
		host.Step(protocol.StepEvent{Action: protocol.StepActionChat, Output: "canonical-answer"})
		return "canonical-answer", nil
	})
	defer backend.Close()
	var wire bytes.Buffer
	s := &server{backend: backend, cwd: "/workspace", active: make(map[string]*activeTurn), conn: newConnection(&wire)}
	result, rpcErr := s.prompt(t.Context(), json.RawMessage(`{"sessionId":"session-1","prompt":[{"type":"text","text":"hello"}]}`))
	if rpcErr != nil {
		t.Fatal(rpcErr)
	}
	if result.(map[string]any)["stopReason"] != "end_turn" {
		t.Fatal(result)
	}
	if output := wire.String(); strings.Contains(output, "discarded-attempt") || strings.Contains(output, "provisional-answer") || strings.Count(output, "canonical-answer") != 1 {
		t.Fatalf("wire output=%s", output)
	}
}

func newPromptContractBackend(run rt.RunnerFunc) *rt.Runtime {
	return rt.New(run, rt.ServiceGroups{
		Models: rt.ModelsServices{
			ModelOptions:        func() ([]rt.ModelOption, string) { return nil, "" },
			CurrentModel:        func() rt.ModelSelection { return rt.ModelSelection{} },
			SwitchSessionModel:  func(string) (rt.ModelSelection, error) { return rt.ModelSelection{}, nil },
			SetSessionReasoning: func(string) error { return nil },
		},
		Permissions: rt.PermissionsServices{
			PermissionMode: func() string { return "manual" },
			SetFullAccess:  func(bool) {},
		},
		Sessions: rt.SessionsServices{
			CurrentSessionID: func() string { return "session-1" },
			ListSessions:     func() []rt.SessionMeta { return []rt.SessionMeta{{ID: "session-1", CWD: "/workspace"}} },
			SessionMessages:  func() []rt.DisplayMessage { return nil },
		},
		MCP: rt.MCPServices{
			ReplaceMCPServers: func(context.Context, string, []rt.MCPServerSpec) error { return nil },
		},
	})
}

// Buffer complete outbound frames so the test acts as a peer without blocking its writer.
type permissionFrames chan []byte

func (w permissionFrames) Write(p []byte) (int, error) {
	w <- bytes.Clone(p)
	return len(p), nil
}

func (w permissionFrames) request(t *testing.T) message {
	t.Helper()
	timeout := time.After(2 * time.Second)
	for {
		select {
		case raw := <-w:
			var frame message
			if err := json.Unmarshal(raw, &frame); err != nil {
				t.Fatal(err)
			}
			if frame.Method == "session/request_permission" {
				return frame
			}
		case <-timeout:
			t.Fatal("missing permission frame")
		}
	}
}

func TestExpiredWorkerPermissionAllowsNextApprovalAndTurnSettlement(t *testing.T) {
	expire := make(chan struct{})
	replies := make(chan rt.ConfirmReply, 2)
	backend := newPromptContractBackend(func(_ context.Context, _ string, host rt.RunHost) (string, error) {
		replies <- host.Confirm(rt.ConfirmRequest{CallID: "expired", ToolName: "read", Done: expire})
		replies <- host.Confirm(rt.ConfirmRequest{CallID: "current", ToolName: "write"})
		return "done", nil
	})
	defer backend.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	wire := make(permissionFrames, 32)
	s := &server{backend: backend, cwd: "/workspace", active: make(map[string]*activeTurn), conn: newConnection(wire)}
	finished := make(chan *wireError, 1)
	go func() {
		_, err := s.prompt(ctx, json.RawMessage(`{"sessionId":"session-1","prompt":[{"type":"text","text":"go"}]}`))
		finished <- err
	}()
	old := wire.request(t)
	close(expire)
	current := wire.request(t)
	if bytes.Equal(old.ID, current.ID) || !bytes.Contains(current.Params, []byte(`"toolCallId":"current"`)) {
		t.Fatalf("unexpected permission request: %s", current.Params)
	}
	// The expired RPC must retire even while the next permission stays open.
	deadline := time.Now().Add(2 * time.Second)
	for {
		s.conn.mu.Lock()
		_, pending := s.conn.pending[string(old.ID)]
		s.conn.mu.Unlock()
		if !pending {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("expired RPC retained until turn exit")
		}
		time.Sleep(time.Millisecond)
	}
	s.conn.resolve(message{ID: old.ID, Result: json.RawMessage(`{"outcome":{"outcome":"selected","optionId":"reject_once"}}`)})
	s.conn.resolve(message{ID: current.ID, Result: json.RawMessage(`{"outcome":{"outcome":"selected","optionId":"allow_once"}}`)})
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("unanswered expired approval blocked prompt settlement")
	}
	if first, second := <-replies, <-replies; !first.Cancelled || !second.Allowed {
		t.Fatalf("expired=%+v current=%+v", first, second)
	}
	if len(s.conn.pending) != 0 {
		t.Fatalf("pending RPCs after settlement: %d", len(s.conn.pending))
	}
}

type pendingPermissionBackend struct{ fakeBackend }

func (b *pendingPermissionBackend) StartRun(context.Context, rt.Input) (rt.RunID, error) {
	b.events <- rt.Event{RunID: "run-1", Type: rt.EventApprovalRequested, Payload: rt.ApprovalView{ID: "pending", ToolName: "read"}}
	return "run-1", nil
}

func TestPromptDisposesUnansweredPermissionOnExit(t *testing.T) {
	for _, exit := range []string{"done", "failed", "cancelled", "disconnect", "stream-closed"} {
		t.Run(exit, func(t *testing.T) {
			b := &pendingPermissionBackend{fakeBackend{current: "s", events: make(chan rt.Event, 2)}}
			wire := make(permissionFrames, 8)
			s := &server{backend: b, active: make(map[string]*activeTurn), conn: newConnection(wire)}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan *wireError, 1)
			go func() {
				_, err := s.prompt(ctx, json.RawMessage(`{"sessionId":"s","prompt":[{"type":"text","text":"go"}]}`))
				done <- err
			}()
			req := wire.request(t)
			switch exit {
			case "disconnect":
				cancel()
			case "stream-closed":
				close(b.events)
			default:
				kind := map[string]rt.EventType{"done": rt.EventRunDone, "failed": rt.EventRunFailed, "cancelled": rt.EventRunCancelled}[exit]
				b.events <- rt.Event{RunID: "run-1", Type: kind, Payload: rt.RunResult{}}
			}
			select {
			case err := <-done:
				wantError := exit == "disconnect" || exit == "failed" || exit == "stream-closed"
				if (err != nil) != wantError {
					t.Fatalf("turn error: %+v", err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("turn exit waited for stale permission")
			}
			if !b.waited || len(s.conn.pending) != 0 || len(s.active) != 0 {
				t.Fatal("turn left pending state")
			}
			s.conn.resolve(message{ID: req.ID, Result: json.RawMessage(`{"outcome":{"outcome":"selected","optionId":"allow_always"}}`)})
		})
	}
}
