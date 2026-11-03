package runner

import (
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/permission"
	"os"
	"path/filepath"
	"testing"
)

func TestSessionApprovalExpiresOnSessionSwitch(t *testing.T) {
	e := newPermTestExecutor(t)
	root := t.TempDir()
	e.SetProjectStore(root)
	e.SetPermissionPolicy(permission.PermissionsDecl{}, root, root)
	e.workspace.SetSession("A")
	c := core.ToolCallItem{Name: "shell", Args: map[string]any{"command": "curl https://a.test > out.json"}}
	if err := e.rememberAllowRule(c.Name, c.Args); err != nil {
		t.Fatal(err)
	}
	e.SetPermissionPolicy(permission.PermissionsDecl{}, root, root)
	if d := e.evaluatePermission(t.Context(), c, nil); d.prompt || d.block {
		t.Fatalf("policy reload dropped current session approval: %+v", d)
	}
	e.workspace.SetSession("B")
	if d := e.evaluatePermission(t.Context(), c, nil); !d.prompt {
		t.Fatalf("session B reused session A approval: %+v", d)
	}
	e.workspace.SetSession("A")
	if d := e.evaluatePermission(t.Context(), c, nil); !d.prompt {
		t.Fatalf("returning to old session resurrected approval: %+v", d)
	}
}

func TestFailedApprovalLeavesNoSessionGrant(t *testing.T) {
	e := newPermTestExecutor(t)
	root := t.TempDir()
	e.SetProjectStore(root)
	e.SetPermissionPolicy(permission.PermissionsDecl{}, root, root)
	dir := filepath.Dir(e.permStore.Path())
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "permissions.json")
	if err := os.WriteFile(path, []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	c := core.ToolCallItem{Name: "shell", Args: map[string]any{"command": "curl https://a.test > out.json"}}
	if err := e.rememberAllowRule(c.Name, c.Args); err == nil {
		t.Fatal("expected save failure")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	e.SetPermissionPolicy(permission.PermissionsDecl{}, root, root)
	if d := e.evaluatePermission(t.Context(), c, nil); !d.prompt {
		t.Fatalf("failed approval became active: %+v", d)
	}
}
