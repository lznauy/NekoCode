package runner

import (
	"context"
	tools "nekocode/bot/extension/tool"
	writetool "nekocode/bot/extension/tool/builtin/filesystem/write"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/permission"
	"nekocode/protocol"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFilePreviewCannotReuseContentFromDifferentTarget(t *testing.T) {
	root := t.TempDir()
	oldPath, newPath := filepath.Join(root, "old.txt"), filepath.Join(root, "public.txt")
	for path, content := range map[string]string{oldPath: privatePreviewMarker + "\n", newPath: "public contents\n"} {
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	tool := &writetool.WriteTool{}
	e := previewRuleExecutor(t, root, tool, tool.PreviewContext)
	calls := []core.ToolCallItem{{Name: "write", Args: map[string]any{"path": oldPath, "content": "replacement\n"}}}
	e.PreparePreviewsContext(t.Context(), calls)
	if got, _ := calls[0].Args["_preview"].(string); !strings.Contains(got, privatePreviewMarker) {
		t.Fatal("setup did not produce old target preview")
	}
	e.SetPermissionPolicy(permission.PermissionsDecl{Deny: []string{"write(/old.txt)"}}, root, root)
	calls[0].Args["path"] = newPath
	var emitted string
	e.SetPreviewDecisionFn(func(_ string, _ string, _ map[string]any, preview string, _ protocol.ToolDecision) { emitted = preview })
	e.emitPreview(t.Context(), calls[0])
	if strings.Contains(emitted, privatePreviewMarker) || !strings.Contains(emitted, "public contents") {
		t.Fatalf("preview from a different target was reused: %q", emitted)
	}
}

func TestManagedImagePreviewHonorsCanonicalDeny(t *testing.T) {
	tool := &captureTool{fakeTool: fakeTool{name: "image_gen"}}
	e, _, _, home := managedImageFixture(t, tool)
	called := 0
	e.registry.RegisterWithOptions(tool, tools.RegistrationOptions{Preview: func(context.Context, map[string]any) string { called++; return privatePreviewMarker }})
	e.SetPermissionPolicy(permission.PermissionsDecl{Deny: []string{"image_gen(/private/**)"}}, e.workspace.Primary(), home)
	calls := []core.ToolCallItem{{Name: "image_gen", Args: map[string]any{"prompt": "test"}}}
	e.PreparePreviewsContext(t.Context(), calls)
	if called != 0 || calls[0].Args["_preview"] != nil {
		t.Fatalf("managed target deny bypassed by preview: called=%d args=%v", called, calls[0].Args)
	}
}
