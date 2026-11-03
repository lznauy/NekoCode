package runner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/permission"
	"nekocode/protocol"
)

type dispatchModeChangeContext struct {
	context.Context
	executor *Executor
	checks   int
}

func (c *dispatchModeChangeContext) Err() error {
	c.checks++
	if c.checks == 2 {
		c.executor.SetPermissionMode(permission.ModeManual)
	}
	return nil
}

func TestModeChangeAtDispatchInvalidatesApproval(t *testing.T) {
	e := newPermTestExecutor(t)
	root := t.TempDir()
	e.workspace.Configure(root, nil)
	e.SetProjectStore(root)
	e.SetPermissionPolicy(permission.PermissionsDecl{Ask: []string{"write"}}, root, root)
	e.SetPermissionMode(permission.ModeFull)
	ctx := &dispatchModeChangeContext{Context: t.Context(), executor: e}
	result := e.executeOne(ctx, core.ToolCallItem{Name: "write", Args: map[string]any{"path": filepath.Join(root, "file"), "content": "unused"}})
	if result.Error != errApprovalContextChanged.Error() || len(result.Evidence.Executions) != 0 {
		t.Fatalf("stale full-mode approval reached dispatch: checks=%d result=%+v", ctx.checks, result)
	}
}

func TestPrivilegedRetryRejectsChangedFileTargetBeforeRemembering(t *testing.T) {
	for _, remember := range []bool{false, true} {
		t.Run(map[bool]string{false: "once", true: "remember"}[remember], func(t *testing.T) {
			root := t.TempDir()
			public, private := filepath.Join(root, "public"), filepath.Join(root, "private")
			for _, dir := range []string{public, private} {
				if err := os.Mkdir(dir, 0700); err != nil {
					t.Fatal(err)
				}
			}
			probe := filepath.Join(root, "probe")
			if err := os.Symlink(private, probe); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			if err := os.Remove(probe); err != nil {
				t.Fatal(err)
			}
			req := core.PermissionRequest{Capabilities: []string{core.CapNetOutbound}, Scope: "project"}
			tool := &permissionTool{fakeTool: fakeTool{name: "write", mode: core.ModeSequential}, req: req}
			e := newTestExecutor(fakeRegistry{"write": tool})
			e.workspace.Configure(root, nil)
			e.SetProjectStore(root)
			e.SetPermissionPolicy(permission.PermissionsDecl{Deny: []string{"write(/private/**)"}}, root, root)
			confirmed := false
			e.SetConfirmFn(func(protocol.ConfirmRequest) protocol.ConfirmReply {
				confirmed = true
				if err := os.Rename(public, public+"-old"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(private, public); err != nil {
					t.Fatal(err)
				}
				if remember {
					return protocol.AllowRemembered()
				}
				return protocol.AllowOnce()
			})
			result := e.ExecuteBatch(t.Context(), []core.ToolCallItem{{Name: "write", Args: map[string]any{"path": filepath.Join(public, "file"), "content": "unused"}}})[0]
			if !confirmed || tool.privilegedCalls != 0 || !strings.Contains(result.Error, "file target changed") || len(result.Evidence.Executions) != 1 {
				t.Errorf("changed target reached privileged dispatch: confirmed=%t calls=%d result=%+v", confirmed, tool.privilegedCalls, result)
			}
			if decision := e.permStore.Decide([]string{"write"}, req); decision.Err != nil || decision.Allow {
				t.Errorf("changed target left an authorization behind: %+v", decision)
			}
		})
	}
}
