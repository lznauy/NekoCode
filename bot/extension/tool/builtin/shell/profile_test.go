package shell

import (
	"context"
	"slices"
	"testing"

	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/sandbox"
	"nekocode/bot/extension/tool/runtime/workspace"
)

func TestPermissionPlanUsesShellSandboxSchema(t *testing.T) {
	tool := &ShellTool{}
	request := tool.PermissionPlan(map[string]any{
		"network":        true,
		"writable_roots": []any{"/cache"},
	}, "/repo")
	if request == nil {
		t.Fatal("expected permission plan")
	}
	if !slices.Equal(request.Capabilities, []string{core.CapNetOutbound, core.CapFsWritePath}) {
		t.Fatalf("capabilities = %v", request.Capabilities)
	}
	if request.Details["workspace"] != "/repo" {
		t.Fatalf("details = %v", request.Details)
	}
}

func TestPermissionPlanHostIsRememberableScoped(t *testing.T) {
	// Host grants are rememberable like any other capability: jev-safe can
	// grant them automatically, so an explicit user "always allow" must be
	// offerable in the approval UI (scope "project", not "once").
	tool := &ShellTool{}
	request := tool.PermissionPlan(map[string]any{
		"sandbox_mode":   "host",
		"network":        true,
		"writable_roots": []any{"/cache"},
	}, "/repo")
	if request == nil || !slices.Equal(request.Capabilities, []string{core.CapProcessHost}) {
		t.Fatalf("request = %+v", request)
	}
	if request.Scope != "project" {
		t.Fatalf("scope = %q, want project (rememberable)", request.Scope)
	}
}

func TestApplyWorkspaceRoots(t *testing.T) {
	cwd := t.TempDir()
	extraRW := t.TempDir()
	extraRO := t.TempDir()
	manager := workspace.New(cwd, []workspace.Root{
		{Path: extraRW, Access: workspace.AccessReadWrite},
		{Path: extraRO, Access: workspace.AccessReadOnly},
	})

	profile := sandbox.Profile{Workspace: cwd}
	ctx := workspace.WithManager(context.Background(), manager)
	if err := applyWorkspaceRoots(ctx, &profile, cwd); err != nil {
		t.Fatal(err)
	}

	if len(profile.WritePaths) != 1 || profile.WritePaths[0] != extraRW {
		t.Fatalf("read-write root should become a WritePath, got %v", profile.WritePaths)
	}
	if len(profile.ReadPaths) != 1 || profile.ReadPaths[0] != extraRO {
		t.Fatalf("read-only root should become a ReadPath, got %v", profile.ReadPaths)
	}
}

func TestApplyWorkspaceRootsSkipsCwd(t *testing.T) {
	cwd := t.TempDir()
	manager := workspace.New(cwd, nil)

	profile := sandbox.Profile{Workspace: cwd}
	ctx := workspace.WithManager(context.Background(), manager)
	if err := applyWorkspaceRoots(ctx, &profile, cwd); err != nil {
		t.Fatal(err)
	}

	if len(profile.WritePaths) != 0 || len(profile.ReadPaths) != 0 {
		t.Fatalf("primary workspace must not be duplicated: write=%v read=%v",
			profile.WritePaths, profile.ReadPaths)
	}
}
