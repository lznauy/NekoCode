package agent

import (
	"nekocode/bot/extension/tool/builtin/shell"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/permission"
	"nekocode/bot/policy"
	"nekocode/protocol"
	"testing"
)

func TestMainAgentPermissionObserverIsWired(t *testing.T) {
	agent := newTestAgent()
	agent.deps.toolRegistry.Register(&shell.ShellTool{})
	agent.Executor().SetPermissionPolicy(permission.PermissionsDecl{Deny: []string{"shell"}}, t.TempDir(), t.TempDir())
	observed := false
	if err := agent.Governance().Register(policy.Hook{Name: "observe-denial", Point: policy.PermissionDenied, On: func(state policy.State) *policy.Result { observed = state.Facts().ActorID == "main"; return nil }}); err != nil {
		t.Fatal(err)
	}
	result := agent.Executor().ExecuteBatch(t.Context(), []core.ToolCallItem{{ID: "call", Name: "shell", Args: map[string]any{"command": "true"}}})[0]
	if result.Error == "" || !observed {
		t.Fatalf("wired=%v result=%+v", observed, result)
	}
}

func TestMainAgentWithoutApprovalHandlerDoesNotReportUserDenial(t *testing.T) {
	agent := newTestAgent()
	agent.deps.toolRegistry.Register(&shell.ShellTool{})
	agent.Executor().SetPermissionPolicy(permission.PermissionsDecl{Ask: []string{"shell"}}, t.TempDir(), t.TempDir())
	events := 0
	for _, point := range []policy.HookPoint{policy.PermissionRequest, policy.PermissionDenied} {
		if err := agent.Governance().Register(policy.Hook{Name: string(point), Point: point, On: func(policy.State) *policy.Result { events++; return nil }}); err != nil {
			t.Fatal(err)
		}
	}
	if agent.ConfirmFn() != nil {
		t.Fatal("missing UI was replaced with denial callback")
	}
	agent.Executor().ExecuteBatch(t.Context(), []core.ToolCallItem{{ID: "call", Name: "shell", Args: map[string]any{"command": "fixture"}}})
	if events != 0 {
		t.Fatalf("headless execution fabricated %d approval events", events)
	}
	restore := agent.BindInteraction(Interaction{Confirm: func(protocol.ConfirmRequest) protocol.ConfirmReply { return protocol.Deny() }})
	agent.Executor().ExecuteBatch(t.Context(), []core.ToolCallItem{{ID: "call", Name: "shell", Args: map[string]any{"command": "fixture"}}})
	if events != 2 {
		t.Fatalf("real interaction did not emit events: %d", events)
	}
	restore()
	if agent.ConfirmFn() != nil {
		t.Fatal("restoring interaction lost missing-handler state")
	}
}
