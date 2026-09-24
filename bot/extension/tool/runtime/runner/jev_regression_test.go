package runner

import (
	"context"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/permission"
	"nekocode/protocol"
	"testing"
)

func TestUnavailableURLJudgeNeverReportsSafe(t *testing.T) {
	e := NewExecutor(nil)
	e.SetPermissionMode(permission.ModeAuto)
	e.SetWebAutoJudge(func(context.Context, string) (bool, bool) { return false, false })
	var decision protocol.ToolDecision
	e.SetPreviewDecisionFn(func(_ string, _ string, _ map[string]any, _ string, d protocol.ToolDecision) { decision = d })
	e.applyAutoWebGate(context.Background(), permissionDecision{}, core.ToolCallItem{Name: "web_fetch", Args: map[string]any{"url": "https://example.com", "_preview": "GET"}})
	if decision != protocol.ToolDecisionJevURLUnavailable {
		t.Fatalf("unavailable judge decision = %q", decision)
	}
}
func TestShellJudgeCalledOncePerToolCall(t *testing.T) {
	e := NewExecutor(nil)
	e.SetPermissionMode(permission.ModeAuto)
	calls := 0
	e.SetBashAutoJudge(func(context.Context, string) (bool, bool) { calls++; return false, true })
	e.evaluatePermission(context.Background(), core.ToolCallItem{Name: "shell", Args: map[string]any{"command": "make all"}}, nil)
	if calls != 1 {
		t.Fatalf("judge calls=%d want 1", calls)
	}
}

func TestChildInheritsExplicitAskAndDeny(t *testing.T) {
	parent := NewExecutor(nil)
	parent.SetProjectStore(t.TempDir())
	parent.SetPermissionMode(permission.ModeAuto)
	parent.SetPermissionPolicy(permission.PermissionsDecl{Ask: []string{"shell(git push *)"}, Deny: []string{"shell(rm *)"}}, t.TempDir(), t.TempDir())
	parent.SetBashAutoJudge(func(context.Context, string) (bool, bool) { return false, true })
	child := NewExecutor(nil)
	parent.ConfigureChildPermissions(child)
	for _, tc := range []struct {
		command       string
		prompt, block bool
	}{{"git push origin main", true, false}, {"rm temp", false, true}} {
		d := child.evaluatePermission(context.Background(), core.ToolCallItem{Name: "shell", Args: map[string]any{"command": tc.command}}, nil)
		if d.prompt != tc.prompt || d.block != tc.block {
			t.Fatalf("child lost policy for %s: %+v", tc.command, d)
		}
	}
}

func TestJevPreviewWithoutRegistryPreviewDoesNotMutateArgs(t *testing.T) {
	e := NewExecutor(nil)
	e.SetPermissionMode(permission.ModeAuto)
	e.SetBashAutoJudge(func(context.Context, string) (bool, bool) { return false, true })
	var previews []string
	var decisions []protocol.ToolDecision
	e.SetPreviewDecisionFn(func(_ string, _ string, _ map[string]any, p string, d protocol.ToolDecision) {
		previews = append(previews, p)
		decisions = append(decisions, d)
	})
	args := map[string]any{"command": "make all"}
	d := e.evaluatePermission(context.Background(), core.ToolCallItem{ID: "test", Name: "shell", Args: args}, nil)
	if d.prompt || d.block || len(previews) != 1 || len(decisions) != 1 || decisions[0] != protocol.ToolDecisionJevSafe {
		t.Fatalf("safe verdict missing: %+v previews=%v decisions=%v", d, previews, decisions)
	}
	if _, ok := args["_preview"]; ok {
		t.Fatal("display note leaked into tool execution args")
	}
}

func TestChildPermissionModeTracksParent(t *testing.T) {
	parent, child := NewExecutor(nil), NewExecutor(nil)
	parent.SetProjectStore(t.TempDir())
	parent.SetPermissionMode(permission.ModeAuto)
	calls := 0
	parent.SetBashAutoJudge(func(context.Context, string) (bool, bool) { calls++; return false, true })
	parent.ConfigureChildPermissions(child)
	parent.SetPermissionMode(permission.ModeManual)
	d := child.evaluatePermission(context.Background(), core.ToolCallItem{Name: "shell", Args: map[string]any{"command": "make all"}}, nil)
	if !d.prompt || calls != 0 {
		t.Fatalf("child retained auto after manual switch: decision=%+v calls=%d", d, calls)
	}
	parent.SetPermissionMode(permission.ModeFull)
	if !child.FullAccess() {
		t.Fatal("child did not track full mode")
	}
	parent.SetPermissionMode(permission.ModeManual)
	if child.FullAccess() {
		t.Fatal("child retained full access")
	}
}
