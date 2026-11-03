package runner

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	tools "nekocode/bot/extension/tool"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/permission"
	"nekocode/protocol"
)

func effectiveFilePreviewFixture(t *testing.T, root, path string, resolver bool, preview func(context.Context, map[string]any) string) (*Executor, core.ToolCallItem, map[string]any) {
	t.Helper()
	targetArgs := map[string]any{"path": path, "content": "replacement"}
	args := map[string]any{"action": "replace", "payload": targetArgs}
	call := core.ToolCallItem{Name: "file_proxy", Args: args}
	e := previewRuleExecutor(t, root, fakeTool{name: call.Name}, preview)
	if resolver {
		e.registry.RegisterWithOptions(fakeTool{name: call.Name}, tools.RegistrationOptions{Preview: preview, ResolveTarget: func(map[string]any) (tools.CallTarget, bool) {
			return tools.CallTarget{Name: "write", Args: targetArgs}, true
		}})
	} else {
		call.EffectiveName = "write"
		call.EffectiveArgs = targetArgs
	}
	return e, call, targetArgs
}

func TestEffectiveFilePreviewHonorsCanonicalDeny(t *testing.T) {
	for _, resolver := range []bool{false, true} {
		t.Run(map[bool]string{false: "attached", true: "resolver"}[resolver], func(t *testing.T) {
			root := t.TempDir()
			private, alias := filepath.Join(root, "private"), filepath.Join(root, "alias")
			if err := os.Mkdir(private, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(private, alias); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			providers := 0
			e, call, _ := effectiveFilePreviewFixture(t, root, filepath.Join(alias, "file"), resolver, func(context.Context, map[string]any) string { providers++; return privatePreviewMarker })
			e.SetPermissionPolicy(permission.PermissionsDecl{Deny: []string{"write(/private/**)"}}, root, root)
			call.Args["_preview"] = "obsolete preview"
			calls := []core.ToolCallItem{call}
			e.PreparePreviewsContext(t.Context(), calls)
			call = calls[0]
			if providers != 0 {
				t.Errorf("effective canonical deny invoked provider %d times", providers)
			}
			if got, _ := call.Args["_preview"].(string); got != "" {
				t.Errorf("effective canonical deny cached preview: %q", got)
			}
		})
	}
}

func TestEffectiveFilePreviewRejectsOutsideWorkspaceWithoutGrant(t *testing.T) {
	for _, resolver := range []bool{false, true} {
		t.Run(map[bool]string{false: "attached", true: "resolver"}[resolver], func(t *testing.T) {
			root := t.TempDir()
			outside := filepath.Join(t.TempDir(), "file")
			providers := 0
			e, call, _ := effectiveFilePreviewFixture(t, root, outside, resolver, func(context.Context, map[string]any) string { providers++; return privatePreviewMarker })
			before := len(e.workspace.Snapshot())
			e.SetConfirmFn(func(protocol.ConfirmRequest) protocol.ConfirmReply {
				t.Fatal("preview requested workspace authority")
				return protocol.Deny()
			})
			calls := []core.ToolCallItem{call}
			e.PreparePreviewsContext(t.Context(), calls)
			call = calls[0]
			if providers != 0 {
				t.Errorf("outside effective file invoked provider %d times", providers)
			}
			if got, _ := call.Args["_preview"].(string); got != "" {
				t.Errorf("outside effective target cached preview: %q", got)
			}
			if len(e.workspace.Snapshot()) != before {
				t.Fatal("preview expanded workspace authority")
			}
		})
	}
}

func TestEffectiveFilePreviewKeepsOriginalProxyProtocol(t *testing.T) {
	for _, resolver := range []bool{false, true} {
		t.Run(map[bool]string{false: "attached", true: "resolver"}[resolver], func(t *testing.T) {
			root := t.TempDir()
			directory := filepath.Join(root, "data")
			if err := os.Mkdir(directory, 0700); err != nil {
				t.Fatal(err)
			}
			alias := filepath.Join(root, "alias")
			if err := os.Symlink(directory, alias); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			path := filepath.Join(alias, "file")
			providers := 0
			e, call, targetArgs := effectiveFilePreviewFixture(t, root, path, resolver, func(_ context.Context, args map[string]any) string {
				providers++
				if args["action"] != "replace" {
					t.Errorf("provider received effective args instead of proxy protocol: %+v", args)
				}
				payload, ok := args["payload"].(map[string]any)
				if !ok || payload["path"] != path || payload["content"] != "replacement" {
					t.Errorf("provider proxy payload was rewritten: %+v", args)
				}
				if _, ok := args["path"]; ok {
					t.Error("effective path injected into proxy args")
				}
				return "safe preview"
			})
			calls := []core.ToolCallItem{call}
			e.PreparePreviewsContext(t.Context(), calls)
			call = calls[0]
			if providers != 1 || call.Args["_preview"] != "safe preview" {
				t.Fatalf("allowed effective target lost preview: providers=%d args=%+v", providers, call.Args)
			}
			if targetArgs["path"] != path {
				t.Fatalf("canonical checks mutated actual effective args: %+v", targetArgs)
			}
		})
	}
}

func TestEffectiveFilePreviewTargetChangeDiscardsProviderOutput(t *testing.T) {
	for _, resolver := range []bool{false, true} {
		t.Run(map[bool]string{false: "attached", true: "resolver"}[resolver], func(t *testing.T) {
			root := t.TempDir()
			original, replacement := filepath.Join(root, "original"), filepath.Join(root, "replacement")
			for _, dir := range []string{original, replacement} {
				if err := os.Mkdir(dir, 0700); err != nil {
					t.Fatal(err)
				}
			}
			probe := filepath.Join(root, "probe")
			if err := os.Symlink(replacement, probe); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			if err := os.Remove(probe); err != nil {
				t.Fatal(err)
			}
			providers, emitted := 0, 0
			e, call, _ := effectiveFilePreviewFixture(t, root, filepath.Join(original, "file"), resolver, func(context.Context, map[string]any) string {
				providers++
				if err := os.Rename(original, original+"-old"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(replacement, original); err != nil {
					t.Fatal(err)
				}
				return privatePreviewMarker
			})
			e.SetPreviewDecisionFn(func(string, string, map[string]any, string, protocol.ToolDecision) { emitted++ })
			e.emitPreview(t.Context(), call)
			if providers != 1 || emitted != 0 {
				t.Errorf("changed effective target preview emitted: providers=%d emitted=%d", providers, emitted)
			}
			if got, _ := call.Args["_preview"].(string); got != "" {
				t.Errorf("changed effective target preview cached: %q", got)
			}
		})
	}
}
