package acp

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"nekocode/protocol"
	rt "nekocode/runtime"
)

func TestPromptRealRuntimeDiscardsRetriedText(t *testing.T) {
	backend := rt.New(rt.RunnerFunc(func(_ context.Context, _ string, host rt.RunHost) (string, error) {
		host.Text("discarded-attempt")
		host.(rt.RunTextResetter).ResetText()
		host.Text("provisional-answer")
		host.Step(protocol.StepEvent{Action: protocol.StepActionChat, Output: "canonical-answer"})
		return "canonical-answer", nil
	}), rt.Services{
		CurrentSessionID:    func() string { return "session-1" },
		ListSessions:        func() []rt.SessionMeta { return []rt.SessionMeta{{ID: "session-1", CWD: "/workspace"}} },
		SessionMessages:     func() []rt.DisplayMessage { return nil },
		ModelOptions:        func() ([]rt.ModelOption, string) { return nil, "" },
		CurrentModel:        func() rt.ModelSelection { return rt.ModelSelection{} },
		PermissionMode:      func() string { return "manual" },
		SwitchSessionModel:  func(string) (rt.ModelSelection, error) { return rt.ModelSelection{}, nil },
		SetSessionReasoning: func(string) error { return nil }, SetFullAccess: func(bool) {},
		ReplaceMCPServers: func(context.Context, string, []rt.MCPServerSpec) error { return nil },
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
