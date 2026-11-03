package runner

import (
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/permission"
	"nekocode/protocol"
	"os"
	"path/filepath"
	"testing"
)

func TestFileDenialUsesExecutedPath(t *testing.T) {
	root := t.TempDir()
	private := filepath.Join(root, "private")
	if err := os.Mkdir(private, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(private, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	e := newPermTestExecutor(t)
	e.workspace.Configure(root, nil)
	e.SetProjectStore(root)
	e.SetPermissionPolicy(permission.PermissionsDecl{Deny: []string{"write(/private/**)"}}, root, root)
	for _, path := range []string{filepath.Join(private, "key"), "private/key", filepath.Join(root, "link", "key"), root + "/public/../private/key"} {
		r := e.ExecuteBatch(t.Context(), []core.ToolCallItem{{ID: "audit", Name: "write", Args: map[string]any{"path": path, "content": "x"}}})[0]
		if r.Error != "denied by rule write(/private/**)" {
			t.Errorf("target via %s must be denied by the path rule: %+v", path, r)
		}
	}
}

func TestMCPRememberHasEffect(t *testing.T) {
	e := newPermTestExecutor(t)
	root := t.TempDir()
	e.SetProjectStore(root)
	e.SetPermissionPolicy(permission.PermissionsDecl{}, root, root)
	c := core.ToolCallItem{Name: "capability", Args: map[string]any{}, EffectiveName: "mcp__server__search", EffectiveArgs: map[string]any{"query": "foo"}}
	d := e.evaluatePermission(t.Context(), c, nil)
	reply, ok := e.promptConfirm(c, func(req protocol.ConfirmRequest) protocol.ConfirmReply { return protocol.AllowRemembered() }, nil, d)
	if !ok {
		t.Fatal("approval failed")
	}
	if err := e.rememberApprovedRules(reply.policy, reply.rules); err != nil {
		t.Fatal(err)
	}
	if next := e.evaluatePermission(t.Context(), c, nil); next.prompt || next.block {
		t.Errorf("remember was offered but remained ask: rules=%+v", reply.rules)
	}
}
func TestEscalationRechecksMode(t *testing.T) {
	e := newPermTestExecutor(t)
	e.SetPermissionMode(permission.ModeManual)
	tool := &permissionTool{}
	asked := false
	req := core.PermissionRequest{Capabilities: []string{core.CapProcessHost}}
	_, ok, _, _ := e.tryPermissionEscalation(t.Context(), tool.ExecuteWithPermission, core.ToolCallItem{Name: "shell"}, testPermissionError{req: req}, func(protocol.ConfirmRequest) protocol.ConfirmReply { asked = true; return protocol.Deny() }, escalationApproval{}, false, true)
	if ok || tool.privilegedCalls > 0 || !asked {
		t.Errorf("stale auto verdict bypassed manual approval: ok=%v calls=%d asked=%v", ok, tool.privilegedCalls, asked)
	}
}
func TestSafeAutoEscalationNeedsNoUI(t *testing.T) {
	e := newPermTestExecutor(t)
	e.SetPermissionMode(permission.ModeAuto)
	tool := &permissionTool{}
	req := core.PermissionRequest{Capabilities: []string{core.CapNetOutbound}}
	_, ok, reason, _ := e.tryPermissionEscalation(t.Context(), tool.ExecuteWithPermission, core.ToolCallItem{Name: "shell"}, testPermissionError{req: req}, nil, escalationApproval{}, false, true)
	if !ok {
		t.Errorf("safe auto escalation depended on UI: reason=%v", reason)
	}
}

func TestEmptyRememberPlanOnlyOffersOnce(t *testing.T) {
	e := newPermTestExecutor(t)
	call := core.ToolCallItem{Name: "unknown"}
	decision := permissionDecision{prompt: true}
	reply, ok := e.promptConfirm(call, func(req protocol.ConfirmRequest) protocol.ConfirmReply {
		if req.Approval.CanRemember() {
			t.Error("empty plan offered ineffective remember")
		}
		return protocol.AllowRemembered()
	}, nil, decision)
	if !ok || reply.Remember {
		t.Fatal("empty remember plan accepted permanent reply")
	}
}
func TestModeSwitchInvalidatesApprovalSnapshot(t *testing.T) {
	e := newPermTestExecutor(t)
	e.SetPermissionMode(permission.ModeAuto)
	snapshot := e.permissionSnapshot()
	e.SetPermissionMode(permission.ModeManual)
	e.SetPermissionMode(permission.ModeAuto)
	if snapshot.current(e) {
		t.Fatal("mode switch away and back revived old authorization")
	}
	parent, child := newPermTestExecutor(t), newPermTestExecutor(t)
	parent.SetPermissionMode(permission.ModeAuto)
	parent.ConfigureChildPermissions(child)
	snapshot = child.permissionSnapshot()
	parent.SetPermissionMode(permission.ModeManual)
	if snapshot.current(child) {
		t.Fatal("parent mode switch did not invalidate child approval")
	}
}

func TestLexicalAskSurvivesCanonicalAllow(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "public")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "guarded")); err != nil {
		t.Skip(err)
	}
	e := newPermTestExecutor(t)
	e.workspace.Configure(root, nil)
	e.SetProjectStore(root)
	e.SetPermissionPolicy(permission.PermissionsDecl{Ask: []string{"write(/guarded/**)"}}, root, root)
	asked := false
	e.SetConfirmFn(func(req protocol.ConfirmRequest) protocol.ConfirmReply {
		asked = true
		if req.Args["path"] != filepath.Join(target, "key") {
			t.Error("approval did not show executed path")
		}
		return protocol.Deny()
	})
	r := e.ExecuteBatch(t.Context(), []core.ToolCallItem{{Name: "write", Args: map[string]any{"path": "guarded/key"}}})[0]
	if !asked || r.Error == "" {
		t.Fatal("canonical allow bypassed explicit ask on alias")
	}
}
