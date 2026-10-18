package lsp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	lspcore "nekocode/bot/extension/tool/builtin/lsp/core"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/workspace"
)

// missingSpecs exercises the "server not installed" path deterministically:
// every language routes to a binary that cannot exist on PATH.
var missingSpecs = map[string]lspcore.ServerSpec{
	"go": {Command: "no-such-gopls-xyz", LanguageID: "go", Extensions: []string{".go"}, InstallHint: "go install golang.org/x/tools/gopls@latest"},
}

func TestWorkspaceRootUsesPrimaryRatherThanSortedAuthority(t *testing.T) {
	base := t.TempDir()
	primary := filepath.Join(base, "z-project")
	manager := workspace.New(primary, []workspace.Root{{Path: filepath.Join(base, "a-extra"), Access: workspace.AccessReadWrite}})
	if got := workspaceRoot(workspace.WithManager(t.Context(), manager)); got != primary {
		t.Fatalf("root=%q want=%q", got, primary)
	}
}

func TestLSPRejectsCrossProjectReuse(t *testing.T) {
	tool := NewLSPToolWithSpecs(missingSpecs)
	defer tool.Close()
	one := workspace.WithManager(t.Context(), workspace.New(t.TempDir(), nil))
	two := workspace.WithManager(t.Context(), workspace.New(t.TempDir(), nil))
	if _, err := tool.ensureManager(one); err != nil {
		t.Fatal(err)
	}
	if _, err := tool.ensureManager(two); err == nil {
		t.Fatal("cross-project LSP reuse accepted")
	}
}

func testCtx(t *testing.T) (context.Context, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n\nfunc greet() string { return \"hi\" }\n\nfunc main() {\n\t_ = greet()\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return workspace.WithManager(context.Background(), workspace.New(root, nil)), root
}

func TestToolFamilyInterface(t *testing.T) {
	tool := NewLSPTool()
	defer tool.Close()
	want := map[string]int{
		"lsp_definition":  3,
		"lsp_references":  3,
		"lsp_hover":       3,
		"lsp_diagnostics": 1,
	}
	tools := tool.Tools()
	if len(tools) != len(want) {
		t.Fatalf("Tools() = %d, want %d", len(tools), len(want))
	}
	for _, tt := range tools {
		name := tt.Name()
		if wantParams, ok := want[name]; !ok {
			t.Errorf("unexpected tool %q", name)
			continue
		} else if len(tt.Parameters()) != wantParams {
			t.Errorf("%s: Parameters() = %d, want %d", name, len(tt.Parameters()), wantParams)
		}
		if tt.ExecutionMode(nil) != core.ModeParallel {
			t.Errorf("%s: ExecutionMode = %v, want parallel", name, tt.ExecutionMode(nil))
		}
		if tt.Description() == "" {
			t.Errorf("%s: empty description", name)
		}
	}
}

func TestExecuteRejectsMissingArgs(t *testing.T) {
	ctx, _ := testCtx(t)
	tool := NewLSPToolWithSpecs(missingSpecs)
	defer tool.Close()

	_, err := tool.Tools()[0].Execute(ctx, map[string]any{"line": 1})
	if err == nil || !strings.Contains(err.Error(), "file") {
		t.Errorf("missing file: got %v", err)
	}
	_, err = tool.Tools()[0].Execute(ctx, map[string]any{"file": "main.go"})
	if err == nil || !strings.Contains(err.Error(), "line") {
		t.Errorf("missing line: got %v", err)
	}
	_, err = tool.Tools()[0].Execute(ctx, map[string]any{"file": "main.go", "line": float64(1)})
	if err == nil || !strings.Contains(err.Error(), "symbol") {
		t.Errorf("missing symbol: got %v", err)
	}
	_, err = tool.Tools()[0].Execute(ctx, map[string]any{"file": "main.go", "line": float64(0), "symbol": "greet"})
	if err == nil || !strings.Contains(err.Error(), ">= 1") {
		t.Errorf("bad line: got %v", err)
	}
}

// TestExecuteAllowsPathOutsideWorkspace locks in the behavior that LSP queries
// are not restricted to workspace roots: absolute paths (global deps, stdlib)
// pass through and the request proceeds to the server layer.
func TestExecuteAllowsPathOutsideWorkspace(t *testing.T) {
	ctx, _ := testCtx(t)
	tool := NewLSPToolWithSpecs(missingSpecs)
	defer tool.Close()

	outside := filepath.Join(os.TempDir(), "outside-workspace.go")
	_, err := tool.Tools()[0].Execute(ctx, map[string]any{"file": outside, "line": float64(1), "symbol": "x"})
	if err == nil || !strings.Contains(err.Error(), "no-such-gopls-xyz") {
		t.Errorf("outside path: got %v, want it to reach the server layer (install hint)", err)
	}
}

func TestExecuteSurfacesInstallHint(t *testing.T) {
	ctx, root := testCtx(t)
	tool := NewLSPToolWithSpecs(missingSpecs)
	defer tool.Close()

	for _, tt := range tool.Tools() {
		t.Run(tt.Name(), func(t *testing.T) {
			args := map[string]any{"file": filepath.Join(root, "main.go"), "line": float64(3), "symbol": "greet"}
			if tt.Name() == "lsp_diagnostics" {
				args = map[string]any{"file": filepath.Join(root, "main.go")}
			}
			_, err := tt.Execute(ctx, args)
			if err == nil || !strings.Contains(err.Error(), "no-such-gopls-xyz") {
				t.Errorf("%s: got %v, want install hint", tt.Name(), err)
			}
		})
	}
}

func TestCloseIsIdempotentAndRecreatable(t *testing.T) {
	ctx, _ := testCtx(t)
	tool := NewLSPToolWithSpecs(missingSpecs)
	tool.Close()
	tool.Close()
	// A closed tool restarts cleanly on the next use.
	if _, err := tool.Tools()[0].Execute(ctx, map[string]any{"file": "main.go", "line": float64(3), "symbol": "greet"}); err == nil {
		t.Fatal("expected install hint after Close")
	}
}

func TestWorkspaceRootFallsBackToCWD(t *testing.T) {
	if got := workspaceRoot(context.Background()); got == "" {
		t.Fatal("workspaceRoot must fall back to cwd")
	}
}
