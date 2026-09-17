package headless

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"nekocode/protocol"
	rt "nekocode/runtime"
)

func rpc(t *testing.T, h *harness, method string, fields map[string]any) map[string]any {
	t.Helper()
	if fields == nil {
		fields = map[string]any{}
	}
	fields["subtype"] = method
	h.send(t, map[string]any{"type": "control_request", "request_id": method, "request": fields})
	return h.until(t, "control_response")["response"].(map[string]any)
}

func TestManagementModelPermissionsAndDiscovery(t *testing.T) {
	model := "first"
	full := false
	h := newHarnessWithServices(t, func(context.Context, string, rt.RunHost) (string, error) { return "done", nil }, Options{}, rt.Services{
		CurrentModel:       func() rt.ModelSelection { return rt.ModelSelection{Model: model} },
		ModelOptions:       func() ([]rt.ModelOption, string) { return []rt.ModelOption{{Name: "first"}, {Name: "second"}}, model },
		SwitchSessionModel: func(name string) (rt.ModelSelection, error) { model = name; return rt.ModelSelection{Model: name}, nil },
		SetFullAccess:      func(on bool) { full = on }, PermissionMode: func() string {
			if full {
				return "full"
			}
			return "manual"
		},
		ToolNames: func() []string { return []string{"read", "shell"} },
		ExecuteCommand: func(context.Context, string, rt.RunHost) (rt.CommandResult, error) {
			return rt.CommandResult{Action: rt.CommandIgnored}, nil
		},
		CommandMenu: func(context.Context, string) (rt.CommandMenu, bool) {
			return rt.CommandMenu{Items: []rt.CommandMenuItem{{Value: "/help", Label: "Help"}}}, true
		},
		SkillManagementView: func() rt.SkillManagementView {
			return rt.SkillManagementView{MCP: []rt.MCPServerView{{Name: "test", Args: []string{"secret-token"}, Command: "private-command"}}}
		},
	})
	info := rpc(t, h, "server.info", nil)["response"].(map[string]any)
	if len(info["tools"].([]any)) != 2 || len(info["commands"].([]any)) != 1 || info["protocol_version"] != ProtocolVersion {
		t.Fatal(info)
	}
	if f := rpc(t, h, "models.list", nil); f["subtype"] != "success" {
		t.Fatal(f)
	}
	if f := rpc(t, h, "model.set", map[string]any{"model": "second"}); f["response"].(map[string]any)["model"] != "second" {
		t.Fatal(f)
	}
	if f := rpc(t, h, "permissions.set", map[string]any{"full_access": true}); f["response"].(map[string]any)["permission_mode"] != "full" {
		t.Fatal(f)
	}
	f := rpc(t, h, "extensions.list", nil)
	data, _ := json.Marshal(f)
	if strings.Contains(string(data), "secret-token") || strings.Contains(string(data), "private-command") {
		t.Fatal(string(data))
	}
	h.finish(t)
}

func TestManagementBusySteerAndShutdown(t *testing.T) {
	steered := make(chan string, 1)
	h := newHarnessWithServices(t, func(ctx context.Context, _ string, _ rt.RunHost) (string, error) { <-ctx.Done(); return "", ctx.Err() }, Options{}, rt.Services{
		SwitchSessionModel: func(string) (rt.ModelSelection, error) {
			t.Error("model switched during run")
			return rt.ModelSelection{}, nil
		},
		Steer: func(_ context.Context, text string) error { steered <- text; return nil },
	})
	h.send(t, user("one", "run"))
	h.until(t, "system")
	if f := rpc(t, h, "model.set", map[string]any{"model": "other"}); f["subtype"] != "error" {
		t.Fatal(f)
	}
	if f := rpc(t, h, "run.steer", map[string]any{"text": "change direction"}); f["subtype"] != "success" {
		t.Fatal(f)
	}
	if text := <-steered; text != "change direction" {
		t.Fatal(text)
	}
	if f := rpc(t, h, "shutdown", map[string]any{"mode": "cancel"}); f["subtype"] != "success" {
		t.Fatal(f)
	}
	if f := h.until(t, "result"); f["stop_reason"] != "cancelled" {
		t.Fatal(f)
	}
	if err := <-h.done; err != nil {
		t.Fatal(err)
	}
}

func TestManagementSessionsAndRewind(t *testing.T) {
	session := ""
	rewound := ""
	h := newHarnessWithServices(t, func(context.Context, string, rt.RunHost) (string, error) { return "done", nil }, Options{}, rt.Services{
		NewSession:       func() (rt.SessionMeta, error) { session = "new-session"; return rt.SessionMeta{ID: session}, nil },
		CurrentSessionID: func() string { return session }, ResumeSession: func(id string) error { session = id; return nil },
		ListSessions:    func() []rt.SessionMeta { return []rt.SessionMeta{{ID: session}} },
		SessionMessages: func() []rt.DisplayMessage { return []rt.DisplayMessage{{Role: "user", Content: session}} },
		Checkpoints: func() ([]rt.CheckpointInfo, error) {
			return []rt.CheckpointInfo{{ID: "checkpoint-1", Label: "First"}}, nil
		},
		DeleteSession: func(string) error { return nil }, Rewind: func(id string) (string, error) { rewound = id; return "restored", nil },
		ExecuteCommand: func(context.Context, string, rt.RunHost) (rt.CommandResult, error) {
			return rt.CommandResult{Action: rt.CommandIgnored}, nil
		},
		CommandMenu: func(context.Context, string) (rt.CommandMenu, bool) {
			return rt.CommandMenu{Items: []rt.CommandMenuItem{{Value: "/rewind checkpoint-1", Label: "First"}}}, true
		},
	})
	for _, method := range []string{"sessions.list", "session.history", "workspace.checkpoints"} {
		if f := rpc(t, h, method, nil); f["subtype"] != "success" {
			t.Fatal(f)
		}
	}
	if f := rpc(t, h, "session.resume", map[string]any{"session_id": "restored"}); f["subtype"] != "success" {
		t.Fatal(f)
	}
	if f := rpc(t, h, "session.history", nil); f["response"].(map[string]any)["session_id"] != "restored" {
		t.Fatal(f)
	}
	if f := rpc(t, h, "session.delete", map[string]any{"session_id": "restored"}); f["subtype"] != "error" {
		t.Fatal(f)
	}
	if f := rpc(t, h, "workspace.rewind", map[string]any{"checkpoint_id": "checkpoint-1"}); f["subtype"] != "success" || rewound != "checkpoint-1" {
		t.Fatal(f)
	}
	h.send(t, user("one", "run"))
	if f := h.until(t, "result"); f["session_id"] != "restored" {
		t.Fatal(f)
	}
	h.finish(t)
}

func TestRunSummaryDenialsAndSubagentOutput(t *testing.T) {
	h := newHarness(t, func(_ context.Context, _ string, host rt.RunHost) (string, error) {
		host.Step(protocol.StepEvent{Action: protocol.StepActionSubAgentText, SubAgentID: "child", Output: "pre"})
		host.Step(protocol.StepEvent{Action: protocol.StepActionSubAgentMessage, SubAgentID: "child", Output: "child answer"})
		host.Confirm(rt.ConfirmRequest{ToolName: "shell", CallID: "denied"})
		host.Step(protocol.StepEvent{Action: protocol.StepActionRunSummary, Summary: &protocol.RunSummary{StepCount: 150, StopReason: "step_limit"}})
		return "partial", nil
	}, Options{IncludePartialMessages: true})
	h.send(t, user("one", "run"))
	f := h.until(t, "subagent")
	if f["subagent_id"] != "child" || f["event"] != "text_delta" || f["user_message_uuid"] != "one" || f["run_id"] == nil {
		t.Fatal(f)
	}
	f = h.until(t, "subagent")
	if f["text"] != "child answer" {
		t.Fatal(f)
	}
	p := h.until(t, "control_request")
	h.send(t, reply(p["request_id"], map[string]any{"behavior": "deny"}))
	f = h.until(t, "result")
	if f["stop_reason"] != "step_limit" || f["step_count"] != float64(150) || f["is_error"] != true || len(f["permission_denials"].([]any)) != 1 {
		t.Fatal(f)
	}
	h.finish(t)
}

func TestSummaryTerminalPrecedence(t *testing.T) {
	for _, reason := range []string{"completed", "step_limit", "cancelled", "execution_error"} {
		for _, failed := range []bool{false, true} {
			t.Run(reason+fmt.Sprint(failed), func(t *testing.T) {
				h := newHarness(t, func(_ context.Context, _ string, host rt.RunHost) (string, error) {
					host.Step(protocol.StepEvent{Action: protocol.StepActionRunSummary, Summary: &protocol.RunSummary{StepCount: 2, StopReason: reason}})
					if failed {
						return "", errors.New("cleanup failed")
					}
					return "answer", nil
				}, Options{})
				h.send(t, user("one", "run"))
				f := h.until(t, "result")
				want := reason
				if failed {
					want = "execution_error"
				}
				if f["stop_reason"] != want || f["is_error"] != (want != "completed") {
					t.Fatal(f)
				}
				h.finish(t)
			})
		}
	}
}

func TestHistoryLimitsAndFailedResumeBinding(t *testing.T) {
	session := ""
	h := newHarnessWithServices(t, func(context.Context, string, rt.RunHost) (string, error) { return "ok", nil }, Options{}, rt.Services{
		NewSession:       func() (rt.SessionMeta, error) { session = "initial"; return rt.SessionMeta{ID: session}, nil },
		CurrentSessionID: func() string { return session },
		ResumeSession:    func(id string) error { session = id; return errors.New("checkpoint recovery failed") },
		SessionMessages: func() []rt.DisplayMessage {
			return []rt.DisplayMessage{{Content: strings.Repeat("x", 5<<20)}, {Content: strings.Repeat("y", 5<<20)}}
		},
	})
	if f := rpc(t, h, "session.resume", map[string]any{"session_id": "restored"}); f["subtype"] != "error" {
		t.Fatal(f)
	}
	if f := rpc(t, h, "session.history", nil); f["subtype"] != "error" {
		t.Fatal(f)
	}
	f := rpc(t, h, "session.history", map[string]any{"limit": 1})["response"].(map[string]any)
	if f["session_id"] != "restored" || f["has_more"] != true || f["next_offset"] != float64(1) {
		t.Fatal(f)
	}
	if f := rpc(t, h, "server.info", nil); f["subtype"] != "success" {
		t.Fatal(f)
	}
	h.finish(t)
}

func TestInitializeVersion(t *testing.T) {
	h := newHarness(t, func(context.Context, string, rt.RunHost) (string, error) { return "", nil }, Options{})
	if f := rpc(t, h, "initialize", map[string]any{"protocol_version": "unknown"}); f["subtype"] != "error" {
		t.Fatal(f)
	}
	if f := rpc(t, h, "initialize", map[string]any{"protocol_version": ProtocolVersion}); f["subtype"] != "success" {
		t.Fatal(f)
	}
	h.finish(t)
}

func TestShutdownDrainKeepsInteractionsAlive(t *testing.T) {
	h := newHarness(t, func(_ context.Context, _ string, host rt.RunHost) (string, error) {
		if !host.Confirm(rt.ConfirmRequest{ToolName: "shell", CallID: "pending"}).Allowed {
			return "", errors.New("approval rejected")
		}
		return "done", nil
	}, Options{})
	h.send(t, user("one", "run"))
	request := h.until(t, "control_request")
	if f := rpc(t, h, "shutdown", map[string]any{"mode": "drain"}); f["subtype"] != "success" {
		t.Fatal(f)
	}
	h.send(t, user("two", "rejected"))
	for {
		f := h.until(t, "system")
		if f["rejected_uuid"] == "two" {
			break
		}
	}
	h.send(t, reply(request["request_id"], map[string]any{"behavior": "allow"}))
	if f := h.until(t, "result"); f["stop_reason"] != "completed" {
		t.Fatal(f)
	}
	if err := <-h.done; err != nil {
		t.Fatal(err)
	}
}

func TestCapabilitiesAreNotControlMethods(t *testing.T) {
	h := newHarness(t, func(context.Context, string, rt.RunHost) (string, error) { return "ok", nil }, Options{})
	for _, method := range []string{"text", "fifo_turns", "subagent_output", "run_summary", "permission_denials", "can_use_tool", "nekocode_question"} {
		if f := rpc(t, h, method, nil); f["subtype"] != "error" {
			t.Fatalf("%s incorrectly accepted: %v", method, f)
		}
	}
	if f := rpc(t, h, "server.info", nil); f["subtype"] != "success" {
		t.Fatal(f)
	}
	h.finish(t)
}

func TestManagementCapabilitiesMatchIndividualServices(t *testing.T) {
	cases := []struct {
		name     string
		services rt.Services
		want     []string
	}{
		{"current_model_only", rt.Services{CurrentModel: func() rt.ModelSelection { return rt.ModelSelection{Model: "x"} }}, nil},
		{"model_catalog_only", rt.Services{ModelOptions: func() ([]rt.ModelOption, string) { return nil, "" }}, []string{"models.list"}},
		{"read_only_sessions", rt.Services{
			CurrentSessionID: func() string { return "current" },
			ListSessions:     func() []rt.SessionMeta { return nil },
			SessionMessages:  func() []rt.DisplayMessage { return nil },
		}, []string{"sessions.list", "session.history"}},
		{"session_create_only", rt.Services{NewSession: func() (rt.SessionMeta, error) { return rt.SessionMeta{}, nil }}, []string{"session.new"}},
		{"session_resume_only", rt.Services{ResumeSession: func(string) error { return nil }}, []string{"session.resume"}},
		{"session_delete_only", rt.Services{DeleteSession: func(string) error { return nil }}, []string{"session.delete"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			backend := rt.New(rt.RunnerFunc(func(context.Context, string, rt.RunHost) (string, error) { return "", nil }), tc.services)
			defer backend.Close()
			s := &server{backend: backend}
			got := s.capabilities()
			for _, method := range []string{"models.list", "sessions.list", "session.history", "session.new", "session.resume", "session.delete"} {
				if slices.Contains(got, method) != slices.Contains(tc.want, method) {
					t.Errorf("%s: capabilities=%v want management methods=%v", method, got, tc.want)
				}
			}
		})
	}
}

func TestCheckpointsUnavailableReturnsError(t *testing.T) {
	h := newHarnessWithServices(t, func(context.Context, string, rt.RunHost) (string, error) { return "", nil }, Options{}, rt.Services{
		Rewind: func(string) (string, error) { return "ok", nil },
	})
	if f := rpc(t, h, "workspace.checkpoints", nil); f["subtype"] != "error" {
		t.Fatalf("missing checkpoint query reported success: %v", f)
	}
	h.finish(t)
}

func TestCheckpointLookupFailureIsControlError(t *testing.T) {
	h := newHarnessWithServices(t, func(context.Context, string, rt.RunHost) (string, error) { return "", nil }, Options{}, rt.Services{
		Checkpoints: func() ([]rt.CheckpointInfo, error) { return nil, errors.New("checkpoint manifest unreadable") },
	})
	info := rpc(t, h, "server.info", nil)["response"].(map[string]any)
	var methods []string
	for _, value := range info["capabilities"].([]any) {
		methods = append(methods, value.(string))
	}
	if !slices.Contains(methods, "workspace.checkpoints") || slices.Contains(methods, "workspace.rewind") {
		t.Fatal(methods)
	}
	if f := rpc(t, h, "workspace.checkpoints", nil); f["subtype"] != "error" || !strings.Contains(f["error"].(string), "manifest unreadable") {
		t.Fatal(f)
	}
	if f := rpc(t, h, "server.info", nil); f["subtype"] != "success" {
		t.Fatal(f)
	}
	h.finish(t)
}
