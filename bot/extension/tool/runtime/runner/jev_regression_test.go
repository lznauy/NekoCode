package runner

import (
	"context"
	"fmt"
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

func TestShellAutoVerdictSurvivesDynamicAssessment(t *testing.T) {
	for _, command := range []string{`echo "$(pwd)"`, `bash -c 'echo ok'`, `source ./script.sh`, "bash <<'EOF'\necho ok\nEOF"} {
		for _, tc := range []struct {
			name                         string
			dangerous, available, prompt bool
			reason, note                 string
		}{
			{"safe", false, true, false, "", "jev_safe"},
			{"dangerous", true, true, true, "jev: judged dangerous", "jev_dangerous"},
			{"failed", false, false, true, "jev: judgment failed", "jev_failed"},
		} {
			t.Run(command+"/"+tc.name, func(t *testing.T) {
				e := NewExecutor(nil)
				e.SetPermissionMode(permission.ModeAuto)
				calls := 0
				e.SetBashAutoJudge(func(context.Context, string) (bool, bool) { calls++; return tc.dangerous, tc.available })
				var note protocol.ToolDecision
				e.SetPreviewDecisionFn(func(_ string, _ string, _ map[string]any, _ string, d protocol.ToolDecision) { note = d })
				d := e.evaluatePermission(t.Context(), core.ToolCallItem{Name: "shell", Args: map[string]any{"command": command}}, nil)
				if d.block || d.prompt != tc.prompt || d.jevSafe != (!tc.prompt) || calls != 1 || string(note) != tc.note {
					t.Fatalf("decision=%+v note=%q calls=%d", d, note, calls)
				}
				if tc.prompt && (d.approval == nil || d.approval.Risk != tc.reason || len(d.approval.Structures) == 0) {
					t.Fatalf("approval lost verdict or structure: %+v", d.approval)
				}
			})
		}
	}
}

func TestShellFailedJudgeIsVisibleOnlyInAutoMode(t *testing.T) {
	for _, mode := range []permission.Mode{permission.ModeAuto, permission.ModeManual} {
		t.Run(fmt.Sprint(mode), func(t *testing.T) {
			e := NewExecutor(nil)
			e.SetPermissionMode(mode)
			calls := 0
			e.SetBashAutoJudge(func(context.Context, string) (bool, bool) { calls++; return false, false })
			var note protocol.ToolDecision
			e.SetPreviewDecisionFn(func(_ string, _ string, _ map[string]any, _ string, d protocol.ToolDecision) { note = d })
			d := e.evaluatePermission(t.Context(), core.ToolCallItem{Name: "shell", Args: map[string]any{"command": "make all"}}, nil)
			if !d.prompt || d.jevSafe {
				t.Fatalf("failed judge allowed execution: %+v", d)
			}
			if mode == permission.ModeAuto {
				if calls != 1 || string(note) != "jev_failed" || d.approval == nil || d.approval.Risk != "jev: judgment failed" {
					t.Fatalf("missing failure status: decision=%+v note=%q calls=%d", d, note, calls)
				}
			} else if calls != 0 || note != "" {
				t.Fatalf("manual mode incorrectly judged: calls=%d note=%q", calls, note)
			}
		})
	}
}

func TestDynamicShellSafeVerdictDiscardedAfterSwitchToManual(t *testing.T) {
	e := NewExecutor(nil)
	e.SetPermissionMode(permission.ModeAuto)
	entered, release := make(chan struct{}), make(chan struct{})
	e.SetBashAutoJudge(func(context.Context, string) (bool, bool) { close(entered); <-release; return false, true })
	result := make(chan permissionDecision, 1)
	go func() {
		result <- e.evaluatePermission(t.Context(), core.ToolCallItem{Name: "shell", Args: map[string]any{"command": `bash -c 'echo ok'`}}, nil)
	}()
	<-entered
	e.SetPermissionMode(permission.ModeManual)
	close(release)
	d := <-result
	if !d.prompt || d.jevSafe || d.approval == nil || d.approval.Risk != "dynamic shell execution" {
		t.Fatalf("stale auto verdict applied in manual mode: %+v", d)
	}
}

func TestDynamicShellJudgeCannotOverrideExplicitRules(t *testing.T) {
	for _, deny := range []bool{false, true} {
		t.Run(fmt.Sprint(deny), func(t *testing.T) {
			e := NewExecutor(nil)
			e.SetPermissionMode(permission.ModeAuto)
			decl := permission.PermissionsDecl{Ask: []string{"shell"}}
			if deny {
				decl = permission.PermissionsDecl{Deny: []string{"shell"}}
			}
			e.SetPermissionPolicy(decl, t.TempDir(), t.TempDir())
			calls := 0
			e.SetBashAutoJudge(func(context.Context, string) (bool, bool) { calls++; return false, true })
			d := e.evaluatePermission(t.Context(), core.ToolCallItem{Name: "shell", Args: map[string]any{"command": `bash -c 'echo ok'`}}, nil)
			if calls != 0 || d.block != deny || d.prompt == deny || d.jevSafe {
				t.Fatalf("explicit rule bypassed: decision=%+v calls=%d", d, calls)
			}
		})
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
