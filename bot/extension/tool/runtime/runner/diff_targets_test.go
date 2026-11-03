package runner

import (
	tools "nekocode/bot/extension/tool"
	difftool "nekocode/bot/extension/tool/builtin/diff"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/permission"
	"nekocode/bot/extension/tool/runtime/workspace"
	"nekocode/protocol"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newDiffTargetExecutor(t *testing.T, root string, decl permission.PermissionsDecl) *Executor {
	t.Helper()
	registry := tools.New(fakeTool{name: "diff"})
	registry.SetWorkspace(workspace.New(root, nil))
	e := NewExecutor(registry)
	e.SetProjectStore(root)
	e.SetPermissionPolicy(decl, root, root)
	return e
}

func TestDiffDisplayHeaderDoesNotRequestWorkspaceAccess(t *testing.T) {
	root := t.TempDir()
	e := newDiffTargetExecutor(t, root, permission.PermissionsDecl{})
	r := e.ExecuteBatch(t.Context(), []core.ToolCallItem{{Name: "diff", Args: map[string]any{"old": "before", "new": "after", "path": filepath.Join(t.TempDir(), "display-only")}}})[0]
	if r.Error != "" || r.Output == "" {
		t.Fatalf("display header treated as a file: %+v", r)
	}
}

func TestDiffEveryInputGetsWorkspaceApproval(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	e := newDiffTargetExecutor(t, root, permission.PermissionsDecl{})
	confirmations := 0
	e.SetConfirmFn(func(req protocol.ConfirmRequest) protocol.ConfirmReply {
		if req.ToolName != "workspace" {
			t.Fatalf("unexpected prompt: %+v", req)
		}
		confirmations++
		return protocol.AllowOnce()
	})
	r := e.ExecuteBatch(t.Context(), []core.ToolCallItem{{Name: "diff", Args: map[string]any{"old": "inline", "new": "path:" + filepath.Join(outside, "source")}}})[0]
	if confirmations != 1 || r.Error != "" {
		t.Fatalf("second diff input skipped workspace approval: confirms=%d result=%+v", confirmations, r)
	}
}

func TestDiffCanonicalInputStillHonorsPathDeny(t *testing.T) {
	root := t.TempDir()
	private := filepath.Join(root, "private")
	if err := os.Mkdir(private, 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "public")
	if err := os.Symlink(private, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	e := newDiffTargetExecutor(t, root, permission.PermissionsDecl{Deny: []string{"diff(/private/**)"}})
	r := e.ExecuteBatch(t.Context(), []core.ToolCallItem{{Name: "diff", Args: map[string]any{"old": "inline", "new": "path:" + filepath.Join(link, "source"), "path": "display header"}}})[0]
	if !strings.Contains(r.Error, "denied by rule diff(/private/**)") || len(r.Evidence.Executions) != 0 {
		t.Fatalf("canonical diff input ignored: %+v", r)
	}
}

func TestDiffReadsApprovedActualInputsWithoutChangingHeader(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	before, after := filepath.Join(outside, "before"), filepath.Join(outside, "after")
	for path, content := range map[string]string{before: "before\n", after: "after\n"} {
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	registry := tools.New(difftool.NewTool())
	registry.SetWorkspace(workspace.New(root, nil))
	e := NewExecutor(registry)
	e.SetProjectStore(root)
	e.SetPermissionPolicy(permission.PermissionsDecl{}, root, root)
	prompts := 0
	e.SetConfirmFn(func(req protocol.ConfirmRequest) protocol.ConfirmReply {
		if req.ToolName != "workspace" {
			t.Fatalf("unexpected prompt %+v", req)
		}
		prompts++
		return protocol.AllowOnce()
	})
	header := "review title / not a real path"
	args := map[string]any{"old": "path: " + before, "new": "path:" + after, "path": header}
	r := e.ExecuteBatch(t.Context(), []core.ToolCallItem{{Name: "diff", Args: args}})[0]
	if r.Error != "" || prompts != 1 || !strings.Contains(r.Output, "["+header+"#diff]") || !strings.Contains(r.Output, "-1:before") || !strings.Contains(r.Output, "+1:after") {
		t.Fatalf("real diff did not use approved inputs: prompts=%d result=%+v", prompts, r)
	}
	if args["old"] != "path: "+before || args["path"] != header {
		t.Fatal("policy normalization mutated caller arguments")
	}
}

func TestDiffBindsSecondInputAcrossApproval(t *testing.T) {
	root := t.TempDir()
	public, private := filepath.Join(root, "public"), filepath.Join(root, "private")
	for _, path := range []string{public, private} {
		if err := os.Mkdir(path, 0700); err != nil {
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
	e := newDiffTargetExecutor(t, root, permission.PermissionsDecl{Ask: []string{"diff"}})
	e.SetConfirmFn(func(req protocol.ConfirmRequest) protocol.ConfirmReply {
		if req.ToolName != "diff" {
			t.Fatalf("unexpected request: %+v", req)
		}
		if err := os.Rename(public, public+"-old"); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(private, public); err != nil {
			t.Fatal(err)
		}
		return protocol.AllowOnce()
	})
	r := e.ExecuteBatch(t.Context(), []core.ToolCallItem{{Name: "diff", Args: map[string]any{"old": "inline text", "new": "path:" + filepath.Join(public, "source")}}})[0]
	if !strings.Contains(r.Error, "file target changed") || len(r.Evidence.Executions) != 0 {
		t.Fatalf("second input changed without invalidating approval: %+v", r)
	}
}
