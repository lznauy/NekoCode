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

func managedImageFixture(t *testing.T, tool core.Tool) (*Executor, string, string, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	root := t.TempDir()
	target := filepath.Join(root, "private", "out")
	if err := os.MkdirAll(target, 0700); err != nil {
		t.Fatal(err)
	}
	managed := filepath.Join(home, ".nekocode", "images")
	if err := os.MkdirAll(filepath.Dir(managed), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, managed); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	e := newTestExecutor(fakeRegistry{"image_gen": tool})
	e.workspace.Configure(root, nil)
	e.SetProjectStore(root)
	e.SetPermissionPolicy(permission.PermissionsDecl{}, root, home)
	return e, managed, target, home
}

func TestManagedImageDefaultHonorsCanonicalDeny(t *testing.T) {
	tool := &captureTool{fakeTool: fakeTool{name: "image_gen"}}
	e, _, _, home := managedImageFixture(t, tool)
	e.SetPermissionPolicy(permission.PermissionsDecl{Deny: []string{"image_gen(/private/**)"}}, e.workspace.Primary(), home)
	result := e.ExecuteBatch(t.Context(), []core.ToolCallItem{{Name: "image_gen", Args: map[string]any{"prompt": "test"}}})[0]
	if result.Error != "denied by rule image_gen(/private/**)" || tool.seen != nil {
		t.Fatalf("managed target ignored canonical deny: %+v", result)
	}
}

func TestManagedImageDefaultRetargetDuringApprovalStopsDispatch(t *testing.T) {
	tool := &captureTool{fakeTool: fakeTool{name: "image_gen"}}
	e, managed, target, home := managedImageFixture(t, tool)
	e.SetPermissionPolicy(permission.PermissionsDecl{Ask: []string{"image_gen"}}, e.workspace.Primary(), home)
	replacement := t.TempDir()
	confirmed := false
	e.SetConfirmFn(func(req protocol.ConfirmRequest) protocol.ConfirmReply {
		confirmed = true
		if req.ToolName == "workspace" {
			t.Fatal("managed default requested workspace authorization")
		}
		if req.Args["output_dir"] != target {
			t.Errorf("approval omitted canonical managed target: %+v", req.Args)
		}
		if err := os.Remove(managed); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(replacement, managed); err != nil {
			t.Fatal(err)
		}
		return protocol.AllowOnce()
	})
	result := e.ExecuteBatch(t.Context(), []core.ToolCallItem{{Name: "image_gen", Args: map[string]any{"prompt": "test"}}})[0]
	if !confirmed || !strings.Contains(result.Error, "file target changed") || tool.seen != nil || len(result.Evidence.Executions) != 0 {
		t.Fatalf("retargeted managed directory reached execution: %+v", result)
	}
}

func TestManagedImageDefaultPreservesImplicitExecutionAndWorkspace(t *testing.T) {
	tool := &captureTool{fakeTool: fakeTool{name: "image_gen"}}
	e, _, target, home := managedImageFixture(t, tool)
	// The managed output is outside the actual workspace and needs no new root.
	workspaceRoot := t.TempDir()
	e.workspace.Configure(workspaceRoot, nil)
	e.SetProjectStore(workspaceRoot)
	e.SetPermissionPolicy(permission.PermissionsDecl{Ask: []string{"image_gen"}}, workspaceRoot, home)
	before := len(e.workspace.Snapshot())
	prompts := 0
	e.SetConfirmFn(func(req protocol.ConfirmRequest) protocol.ConfirmReply {
		prompts++
		if req.ToolName == "workspace" {
			t.Fatal("managed default requested workspace authorization")
		}
		if req.Args["output_dir"] != target {
			t.Errorf("approval omitted canonical managed target: %+v", req.Args)
		}
		return protocol.AllowOnce()
	})
	args := map[string]any{"prompt": "test"}
	result := e.ExecuteBatch(t.Context(), []core.ToolCallItem{{Name: "image_gen", Args: args}})[0]
	if result.Error != "" || tool.seen == nil || prompts != 1 {
		t.Fatalf("managed default could not run: %+v", result)
	}
	if _, ok := tool.seen["output_dir"]; ok {
		t.Fatalf("actual args lost implicit managed semantics: %+v", tool.seen)
	}
	if _, ok := args["output_dir"]; ok {
		t.Fatal("caller args mutated")
	}
	if len(e.workspace.Snapshot()) != before {
		t.Fatal("managed default expanded workspace authority")
	}
}

type retargetingImagePermissionTool struct {
	permissionTool
	retarget func()
}

func (t *retargetingImagePermissionTool) Execute(ctx context.Context, args map[string]any) (core.ToolOutput, error) {
	t.retarget()
	return t.permissionTool.Execute(ctx, args)
}

func TestManagedImageRetryKeepsOriginalTargetBeforeRemembering(t *testing.T) {
	req := core.PermissionRequest{Capabilities: []string{core.CapNetOutbound}, Scope: "project"}
	tool := &retargetingImagePermissionTool{permissionTool: permissionTool{fakeTool: fakeTool{name: "image_gen"}, req: req}}
	e, managed, _, _ := managedImageFixture(t, tool)
	replacement := t.TempDir()
	tool.retarget = func() {
		if err := os.Remove(managed); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(replacement, managed); err != nil {
			t.Fatal(err)
		}
	}
	e.SetConfirmFn(func(protocol.ConfirmRequest) protocol.ConfirmReply { return protocol.AllowRemembered() })
	result := e.ExecuteBatch(t.Context(), []core.ToolCallItem{{Name: "image_gen", Args: map[string]any{"prompt": "test"}}})[0]
	if !strings.Contains(result.Error, "file target changed") || tool.privilegedCalls != 0 || len(result.Evidence.Executions) != 1 {
		t.Fatalf("retry forgot original managed target: %+v", result)
	}
	if d := e.permStore.Decide([]string{"image_gen"}, req); d.Err != nil || d.Allow {
		t.Fatalf("changed target left remembered capability: %+v", d)
	}
}
