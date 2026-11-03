package runner

import (
	"encoding/json"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/permission"
	"nekocode/protocol"
	"os"
	"path/filepath"
	"testing"
)

func TestCorruptCapabilityStoreFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name                           string
		mode                           permission.Mode
		preapproved, remember, jevSafe bool
	}{
		{name: "manual", mode: permission.ModeManual},
		{name: "full", mode: permission.ModeFull},
		{name: "auto", mode: permission.ModeAuto, jevSafe: true},
		{name: "preapproved once", mode: permission.ModeManual, preapproved: true},
		{name: "preapproved remembered", mode: permission.ModeManual, preapproved: true, remember: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newPermTestExecutor(t)
			root := t.TempDir()
			e.SetProjectStore(root)
			e.SetPermissionMode(tc.mode)
			req := core.PermissionRequest{Capabilities: []string{core.CapProcessHost}}
			path := e.permStore.Path()
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			data, err := json.Marshal(map[string]any{"version": 1, "projects": map[string]any{root: map[string]any{"grants": []permission.Grant{{Effect: "deny", Tool: "shell", Capabilities: req.Capabilities, Workspace: root}}}}})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			tool := &permissionTool{}
			confirmed := false
			confirm := func(protocol.ConfirmRequest) protocol.ConfirmReply {
				confirmed = true
				return protocol.AllowRemembered()
			}
			policy := e.permissionSnapshot()
			approved := escalationApproval{remember: tc.remember, predicted: &req, policy: &policy}
			_, ok, reason, _ := e.tryPermissionEscalation(t.Context(), tool.ExecuteWithPermission, core.ToolCallItem{Name: "shell"}, testPermissionError{req: req}, confirm, approved, tc.preapproved, tc.jevSafe)
			if ok || tool.privilegedCalls != 0 || confirmed || reason != failDeniedByRule {
				t.Fatalf("baseline deny was ineffective: ok=%v calls=%d confirmed=%v reason=%v", ok, tool.privilegedCalls, confirmed, reason)
			}
			if err := os.WriteFile(path, []byte("broken JSON"), 0600); err != nil {
				t.Fatal(err)
			}
			_, ok, reason, cause := e.tryPermissionEscalation(t.Context(), tool.ExecuteWithPermission, core.ToolCallItem{Name: "shell"}, testPermissionError{req: req}, confirm, approved, tc.preapproved, tc.jevSafe)
			if ok || tool.privilegedCalls != 0 || confirmed || reason != failPermissionStore || cause == nil {
				t.Fatalf("corrupt policy must fail before approval or dispatch: ok=%v calls=%d confirmed=%v reason=%v cause=%v", ok, tool.privilegedCalls, confirmed, reason, cause)
			}
		})
	}
}

func TestInheritedModeEpochInvalidatesApproval(t *testing.T) {
	root, child, grandchild := newPermTestExecutor(t), newPermTestExecutor(t), newPermTestExecutor(t)
	root.SetPermissionMode(permission.ModeAuto)
	root.ConfigureChildPermissions(child)
	child.ConfigureChildPermissions(grandchild)
	executors := []*Executor{root, child, grandchild}
	snapshots := make([]permissionSnapshot, len(executors))
	for i, e := range executors {
		snapshots[i] = e.permissionSnapshot()
	}
	root.SetPermissionMode(permission.ModeManual)
	root.SetPermissionMode(permission.ModeAuto)
	for i, e := range executors {
		if snapshots[i].current(e) {
			t.Errorf("executor %d retained approval across mode ABA", i)
		}
		if err := e.rememberApprovedRules(snapshots[i], nil); err == nil {
			t.Errorf("executor %d committed stale approval", i)
		}
	}
}

func TestNoopModeUpdatesPreserveApproval(t *testing.T) {
	root, child, grandchild := newPermTestExecutor(t), newPermTestExecutor(t), newPermTestExecutor(t)
	root.SetPermissionMode(permission.ModeAuto)
	root.ConfigureChildPermissions(child)
	child.ConfigureChildPermissions(grandchild)
	for _, e := range []*Executor{root, child, grandchild} {
		snapshot := e.permissionSnapshot()
		root.SetPermissionMode(permission.ModeAuto)
		root.SetBashAuto(true)
		root.SetFullAccess(false)
		if !snapshot.current(e) {
			t.Fatal("no-op mode update invalidated approval")
		}
		if err := e.rememberApprovedRules(snapshot, nil); err != nil {
			t.Fatal(err)
		}
	}
	root.SetFullAccess(true)
	snapshot := grandchild.permissionSnapshot()
	root.SetFullAccess(true)
	if !snapshot.current(grandchild) {
		t.Fatal("no-op full access invalidated delegated approval")
	}
	root.SetFullAccess(false)
	root.SetFullAccess(true)
	if snapshot.current(grandchild) {
		t.Fatal("full-access ABA retained delegated approval")
	}
}
