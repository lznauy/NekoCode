package runner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tools "nekocode/bot/extension/tool"
	difftool "nekocode/bot/extension/tool/builtin/diff"
	edittool "nekocode/bot/extension/tool/builtin/filesystem/edit"
	writetool "nekocode/bot/extension/tool/builtin/filesystem/write"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/permission"
	"nekocode/bot/extension/tool/runtime/workspace"
	"nekocode/protocol"
)

const privatePreviewMarker = "PRIVATE_PREVIEW_CONTENT"

func previewRuleExecutor(t *testing.T, root string, tool core.Tool, preview func(context.Context, map[string]any) string) *Executor {
	t.Helper()
	registry := tools.New()
	registry.RegisterWithOptions(tool, tools.RegistrationOptions{Preview: preview})
	registry.SetWorkspace(workspace.New(root, nil))
	e := NewExecutor(registry)
	e.SetProjectStore(root)
	e.SetPermissionPolicy(permission.PermissionsDecl{}, root, root)
	return e
}

func TestPrepareRealFilePreviewsHonorsDeny(t *testing.T) {
	for _, name := range []string{"diff", "write", "edit"} {
		for _, alias := range []bool{false, true} {
			label := "lexical"
			if alias {
				label = "canonical"
			}
			t.Run(name+"/"+label, func(t *testing.T) {
				root := t.TempDir()
				private := filepath.Join(root, "private")
				if err := os.Mkdir(private, 0700); err != nil {
					t.Fatal(err)
				}
				target := filepath.Join(private, "secret.txt")
				if err := os.WriteFile(target, []byte(privatePreviewMarker+"\nanchor\n"), 0600); err != nil {
					t.Fatal(err)
				}
				if alias {
					link := filepath.Join(root, "public")
					if err := os.Symlink(private, link); err != nil {
						t.Skipf("symlinks unavailable: %v", err)
					}
					target = filepath.Join(link, "secret.txt")
				}
				var tool core.Tool
				var preview func(context.Context, map[string]any) string
				args := map[string]any{"path": target}
				switch name {
				case "diff":
					v := difftool.NewTool()
					tool = v
					preview = v.PreviewContext
					args = map[string]any{"old": "path:" + target, "new": "replacement\n"}
				case "write":
					v := &writetool.WriteTool{}
					tool = v
					preview = v.PreviewContext
					args["content"] = "replacement\n"
				case "edit":
					v := &edittool.EditTool{}
					tool = v
					preview = v.PreviewContext
					args["oldString"] = "anchor"
					args["newString"] = "replacement"
				}
				e := previewRuleExecutor(t, root, tool, preview)
				e.SetPermissionPolicy(permission.PermissionsDecl{Deny: []string{name + "(/private/**)"}}, root, root)
				calls := []core.ToolCallItem{{ID: "denied", Name: name, Args: args}}
				e.PreparePreviewsContext(t.Context(), calls)
				if got, _ := calls[0].Args["_preview"].(string); strings.Contains(got, privatePreviewMarker) {
					t.Error("denied file contents leaked through prepared preview")
				}
				result := e.ExecuteBatch(t.Context(), calls)[0]
				if !strings.Contains(result.Error, "denied by rule") {
					t.Fatalf("execution did not enforce the baseline deny: %+v", result)
				}
			})
		}
	}
}

func TestAllowedRealFilePreviewStillContainsDiff(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "public.txt")
	if err := os.WriteFile(path, []byte(privatePreviewMarker+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	tool := &writetool.WriteTool{}
	e := previewRuleExecutor(t, root, tool, tool.PreviewContext)
	calls := []core.ToolCallItem{{Name: "write", Args: map[string]any{"path": path, "content": "replacement\n"}}}
	e.PreparePreviewsContext(t.Context(), calls)
	if got, _ := calls[0].Args["_preview"].(string); !strings.Contains(got, privatePreviewMarker) || !strings.Contains(got, "replacement") {
		t.Fatalf("allowed preview lost its useful diff: %q", got)
	}
}

func TestCachedPreviewCannotSurvivePermissionRevocation(t *testing.T) {
	for _, entry := range []string{"prepare", "emit", "jev"} {
		t.Run(entry, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "secret.txt")
			if err := os.WriteFile(path, []byte("source"), 0600); err != nil {
				t.Fatal(err)
			}
			previews, emitted, judged, confirmed := 0, 0, 0, 0
			e := previewRuleExecutor(t, root, fakeTool{name: "write"}, func(context.Context, map[string]any) string { previews++; return privatePreviewMarker })
			calls := []core.ToolCallItem{{ID: "cached", Name: "write", Args: map[string]any{"path": path, "content": "replacement"}}}
			e.PreparePreviewsContext(t.Context(), calls)
			if previews != 1 || calls[0].Args["_preview"] != privatePreviewMarker {
				t.Fatal("setup did not cache an authorized preview")
			}
			e.SetPermissionPolicy(permission.PermissionsDecl{Deny: []string{"write(/secret.txt)"}}, root, root)
			e.SetPermissionMode(permission.ModeAuto)
			e.SetBashAutoJudge(func(context.Context, string) (bool, bool) { judged++; return false, true })
			e.SetWebAutoJudge(func(context.Context, string) (bool, bool) { judged++; return false, true })
			e.SetConfirmFn(func(protocol.ConfirmRequest) protocol.ConfirmReply { confirmed++; return protocol.AllowOnce() })
			e.SetPreviewDecisionFn(func(string, string, map[string]any, string, protocol.ToolDecision) { emitted++ })
			switch entry {
			case "prepare":
				e.PreparePreviewsContext(t.Context(), calls)
			case "emit":
				e.emitPreview(t.Context(), calls[0])
			case "jev":
				e.emitJevNote(t.Context(), calls[0], protocol.ToolDecisionJevSafe)
			}
			if previews != 1 || emitted != 0 || judged != 0 || confirmed != 0 {
				t.Fatalf("revoked preview used a provider or observer: previews=%d emitted=%d judged=%d confirmed=%d", previews, emitted, judged, confirmed)
			}
			if got, _ := calls[0].Args["_preview"].(string); strings.Contains(got, privatePreviewMarker) {
				t.Fatal("revoked cached preview remains reusable in shared arguments")
			}
		})
	}
}

func TestPreviewChecksNeverCallJudgeOrConfirm(t *testing.T) {
	root := t.TempDir()
	judged, confirmed, previews, emitted := 0, 0, 0, 0
	e := previewRuleExecutor(t, root, fakeTool{name: "shell"}, func(context.Context, map[string]any) string { previews++; return "command preview" })
	e.SetPermissionMode(permission.ModeAuto)
	e.SetBashAutoJudge(func(context.Context, string) (bool, bool) { judged++; return false, true })
	e.SetConfirmFn(func(protocol.ConfirmRequest) protocol.ConfirmReply { confirmed++; return protocol.Deny() })
	e.SetPreviewDecisionFn(func(string, string, map[string]any, string, protocol.ToolDecision) { emitted++ })
	call := core.ToolCallItem{Name: "shell", Args: map[string]any{"command": "unknown-command --arg"}}
	calls := []core.ToolCallItem{call}
	e.PreparePreviewsContext(t.Context(), calls)
	call = calls[0]
	e.emitJevNote(t.Context(), call, protocol.ToolDecisionJevSafe)
	if judged != 0 || confirmed != 0 || previews < 1 || emitted != 1 {
		t.Fatalf("preview check caused authorization side effects: judged=%d confirmed=%d previews=%d emitted=%d", judged, confirmed, previews, emitted)
	}
}

func TestPreviewOutsideWorkspaceDoesNotReadOrGrant(t *testing.T) {
	for _, entry := range []string{"prepare", "emit", "jev"} {
		t.Run(entry, func(t *testing.T) {
			root := t.TempDir()
			outside := filepath.Join(t.TempDir(), "private.txt")
			if err := os.WriteFile(outside, []byte(privatePreviewMarker), 0600); err != nil {
				t.Fatal(err)
			}
			providers, emitted, confirmed := 0, 0, 0
			e := previewRuleExecutor(t, root, fakeTool{name: "write"}, func(context.Context, map[string]any) string { providers++; return privatePreviewMarker })
			e.SetConfirmFn(func(protocol.ConfirmRequest) protocol.ConfirmReply { confirmed++; return protocol.AllowRemembered() })
			e.SetPreviewDecisionFn(func(string, string, map[string]any, string, protocol.ToolDecision) { emitted++ })
			before := e.workspace.Snapshot()
			call := core.ToolCallItem{Name: "write", Args: map[string]any{"path": outside, "_preview": privatePreviewMarker}}
			switch entry {
			case "prepare":
				calls := []core.ToolCallItem{call}
				e.PreparePreviewsContext(t.Context(), calls)
				call = calls[0]
			case "emit":
				e.emitPreview(t.Context(), call)
			case "jev":
				e.emitJevNote(t.Context(), call, protocol.ToolDecisionJevSafe)
			}
			if providers != 0 || emitted != 0 || confirmed != 0 {
				t.Fatalf("preview read or granted outside workspace: providers=%d emitted=%d confirmed=%d", providers, emitted, confirmed)
			}
			if after := e.workspace.Snapshot(); len(after) != len(before) {
				t.Fatalf("preview added workspace roots: before=%+v after=%+v", before, after)
			}
			if got, _ := call.Args["_preview"].(string); got != "" {
				t.Fatal("outside-workspace cached preview survived")
			}
		})
	}
}

func TestInlineDiffPreviewIgnoresExternalDisplayHeader(t *testing.T) {
	root := t.TempDir()
	tool := difftool.NewTool()
	e := previewRuleExecutor(t, root, tool, tool.PreviewContext)
	header := filepath.Join(t.TempDir(), "display-only.txt")
	calls := []core.ToolCallItem{{Name: "diff", Args: map[string]any{"old": "before\n", "new": "after\n", "path": header}}}
	e.PreparePreviewsContext(t.Context(), calls)
	preview, _ := calls[0].Args["_preview"].(string)
	if !strings.Contains(preview, "before") || !strings.Contains(preview, "after") || !strings.Contains(preview, header) {
		t.Fatalf("inline diff lost text or display header: %q", preview)
	}
	if calls[0].Args["path"] != header || len(e.workspace.Snapshot()) != 1 {
		t.Fatal("display header mutated execution arguments or workspace authority")
	}
}

func TestPreviewPolicyRevokedInsideProviderIsDiscarded(t *testing.T) {
	for _, entry := range []string{"prepare", "emit", "jev"} {
		t.Run(entry, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "private.txt")
			if err := os.WriteFile(path, []byte(privatePreviewMarker), 0600); err != nil {
				t.Fatal(err)
			}
			providers, emitted := 0, 0
			var e *Executor
			e = previewRuleExecutor(t, root, fakeTool{name: "write"}, func(context.Context, map[string]any) string {
				providers++
				e.SetPermissionPolicy(permission.PermissionsDecl{Deny: []string{"write(/private.txt)"}}, root, root)
				return privatePreviewMarker
			})
			e.SetPreviewDecisionFn(func(string, string, map[string]any, string, protocol.ToolDecision) { emitted++ })
			call := core.ToolCallItem{Name: "write", Args: map[string]any{"path": path, "content": "replacement"}}
			switch entry {
			case "prepare":
				calls := []core.ToolCallItem{call}
				e.PreparePreviewsContext(t.Context(), calls)
				call = calls[0]
			case "emit":
				e.emitPreview(t.Context(), call)
			case "jev":
				e.emitJevNote(t.Context(), call, protocol.ToolDecisionJevSafe)
			}
			if providers != 1 || emitted != 0 {
				t.Fatalf("preview escaped a policy change during provider call: providers=%d emitted=%d", providers, emitted)
			}
			if got, _ := call.Args["_preview"].(string); got != "" {
				t.Fatal("preview cached after provider revoked its permission")
			}
		})
	}
}

func TestPreviewHonorsEffectiveMCPDeny(t *testing.T) {
	for _, resolved := range []bool{false, true} {
		name := "attached"
		if resolved {
			name = "resolver"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			providers := 0
			preview := func(context.Context, map[string]any) string { providers++; return privatePreviewMarker }
			e := previewRuleExecutor(t, root, fakeTool{name: "capability"}, preview)
			call := core.ToolCallItem{Name: "capability", Args: map[string]any{"action": "call"}, EffectiveName: "mcp__server__private", EffectiveArgs: map[string]any{"query": "secret"}}
			if resolved {
				call.EffectiveName = ""
				call.EffectiveArgs = nil
				e.registry.RegisterWithOptions(fakeTool{name: "capability"}, tools.RegistrationOptions{Preview: preview, ResolveTarget: func(map[string]any) (tools.CallTarget, bool) {
					return tools.CallTarget{Name: "mcp__server__private", Args: map[string]any{"query": "secret"}}, true
				}})
			}
			e.SetPermissionPolicy(permission.PermissionsDecl{Deny: []string{"mcp__server__private"}}, root, root)
			calls := []core.ToolCallItem{call}
			e.PreparePreviewsContext(t.Context(), calls)
			call = calls[0]
			e.emitPreview(t.Context(), call)
			e.emitJevNote(t.Context(), call, protocol.ToolDecisionJevSafe)
			if providers != 0 {
				t.Fatalf("effective denied target invoked preview provider %d times", providers)
			}
			if got, _ := call.Args["_preview"].(string); got != "" {
				t.Fatal("effective denied target cached a preview")
			}
		})
	}
}
