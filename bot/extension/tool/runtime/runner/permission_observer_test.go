package runner

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/permission"
	"nekocode/protocol"
)

func TestPermissionObserverDecisionPaths(t *testing.T) {
	for _, tc := range []struct {
		name, command string
		allow, full   bool
		decl          permission.PermissionsDecl
		want          string
	}{
		{name: "user denies", command: "git status", decl: permission.PermissionsDecl{Ask: []string{"shell(git status)"}}, want: "request,user"},
		{name: "user allows", command: "git status", allow: true, decl: permission.PermissionsDecl{Ask: []string{"shell(git status)"}}, want: "request"},
		{name: "rule denies", command: "sudo true", want: "rule"},
		{name: "builtin allows", command: "pwd", want: ""},
		{name: "full skips approval", command: "git commit -m fixture", full: true, want: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newPermTestExecutor(t)
			e.SetPermissionPolicy(tc.decl, "/repo", "/home/user")
			e.SetFullAccess(tc.full)
			var seen []string
			var callbacks int
			e.SetPermissionObserver(func(_ context.Context, event PermissionEvent) {
				if event.Tool != "shell" || event.Args["call_id"] != "call" {
					t.Errorf("wrong identity: %+v", event)
				}
				if event.Denied {
					seen = append(seen, event.Args["source"].(string))
				} else {
					seen = append(seen, "request")
				}
			})
			e.SetConfirmFn(func(protocol.ConfirmRequest) protocol.ConfirmReply {
				callbacks++
				if tc.allow {
					return protocol.AllowOnce()
				}
				return protocol.Deny()
			})
			result := e.ExecuteBatch(t.Context(), []core.ToolCallItem{{ID: "call", Name: "shell", Args: map[string]any{"command": tc.command}}})[0]
			if got := strings.Join(seen, ","); got != tc.want {
				t.Fatalf("events=%q want=%q result=%+v", got, tc.want, result)
			}
			if (callbacks > 0) != (strings.Contains(tc.want, "request")) {
				t.Fatalf("callback count=%d", callbacks)
			}
			if tc.want == "request,user" || tc.want == "rule" {
				if result.Error == "" {
					t.Fatal("denied tool ran")
				}
			} else if result.Error != "" {
				t.Fatal(result.Error)
			}
		})
	}
}

func TestPermissionObserverSnapshotsCannotChangeApproval(t *testing.T) {
	e := newPermTestExecutor(t)
	var calls int
	e.SetPermissionObserver(func(_ context.Context, event PermissionEvent) {
		event.Args["tool_args"].(map[string]any)["command"] = "sudo true"
		if approval, ok := event.Args["approval"].(map[string]any); ok {
			approval["capabilities"] = []any{"process.host"}
		}
		if event.Denied {
			calls++
		}
		panic("observer failure must not approve")
	})
	e.SetConfirmFn(func(req protocol.ConfirmRequest) protocol.ConfirmReply {
		if req.Args["command"] != "git commit -m fixture" {
			t.Errorf("observer changed approval: %+v", req)
		}
		return protocol.Deny()
	})
	result := e.ExecuteBatch(t.Context(), []core.ToolCallItem{{ID: "call", Name: "shell", Args: map[string]any{"command": "git commit -m fixture"}}})[0]
	if result.Error == "" || calls != 1 {
		t.Fatalf("result=%+v denials=%d", result, calls)
	}
}

func TestPermissionObserverEscalationAndWorkspace(t *testing.T) {
	t.Run("unified and broader escalation", func(t *testing.T) {
		tool := &permissionTool{fakeTool: fakeTool{name: "shell", mode: core.ModeSequential}}
		tool.req = core.PermissionRequest{Capabilities: []string{core.CapProcessHost}}
		e := newTestExecutor(fakeRegistry{"shell": tool})
		e.SetPermissionPolicy(permission.PermissionsDecl{}, "/repo", "/home/user")
		var seen []PermissionEvent
		e.SetPermissionObserver(func(_ context.Context, event PermissionEvent) { seen = append(seen, event) })
		count := 0
		e.SetConfirmFn(func(protocol.ConfirmRequest) protocol.ConfirmReply {
			count++
			if count == 1 {
				return protocol.AllowOnce()
			}
			return protocol.Deny()
		})
		result := e.ExecuteBatch(t.Context(), []core.ToolCallItem{{ID: "scope", Name: "shell", Args: map[string]any{"command": "curl https://example.com", "network": true}}})[0]
		if result.Error == "" || len(seen) != 3 || seen[0].Denied || seen[1].Denied || !seen[2].Denied {
			t.Fatalf("events=%+v result=%+v", seen, result)
		}
		if seen[0].Args["approval"].(map[string]any)["combined"] != true {
			t.Fatal("combined approval facts missing")
		}
	})
	t.Run("workspace", func(t *testing.T) {
		root := t.TempDir()
		outside := filepath.Join(t.TempDir(), "outside.txt")
		if err := os.WriteFile(outside, []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
		e := newTestExecutor(fakeRegistry{"read": fakeTool{name: "read"}})
		e.workspace.Configure(root, nil)
		var seen []PermissionEvent
		e.SetPermissionObserver(func(_ context.Context, event PermissionEvent) { seen = append(seen, event) })
		e.SetConfirmFn(func(protocol.ConfirmRequest) protocol.ConfirmReply { return protocol.Deny() })
		result := e.ExecuteBatch(t.Context(), []core.ToolCallItem{{ID: "workspace-call", Name: "read", Args: map[string]any{"path": outside}}})[0]
		if result.Error == "" || len(seen) != 2 || seen[0].Tool != "workspace" || seen[1].Args["source"] != "user" {
			t.Fatalf("events=%+v result=%+v", seen, result)
		}
	})
}

func TestPermissionObserverNoDecisionNoEvent(t *testing.T) {
	e := newPermTestExecutor(t)
	e.SetConfirmFn(nil)
	count := 0
	e.SetPermissionObserver(func(context.Context, PermissionEvent) { count++ })
	e.ExecuteBatch(t.Context(), []core.ToolCallItem{{Name: "shell", Args: map[string]any{"command": "git commit -m fixture"}}})
	e.SetPlanMode(true)
	e.ExecuteBatch(t.Context(), []core.ToolCallItem{{Name: "shell", Args: map[string]any{"command": "sudo true"}}})
	if count != 0 {
		t.Fatalf("missing callback/plan mode mislabeled as denial: %d", count)
	}
}

func TestPermissionObserverRememberedGrantAndCapabilityDenial(t *testing.T) {
	for _, deny := range []bool{false, true} {
		tool := &permissionTool{fakeTool: fakeTool{name: "shell", mode: core.ModeSequential}}
		tool.req = core.PermissionRequest{Capabilities: []string{core.CapNetOutbound}}
		e := newTestExecutor(fakeRegistry{"shell": tool})
		root := t.TempDir()
		e.SetProjectStore(root)
		e.SetPermissionPolicy(permission.PermissionsDecl{}, root, t.TempDir())
		if deny {
			path := filepath.Join(root, ".nekocode", "permissions.json")
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			data, err := json.Marshal(map[string]any{"version": 1, "projects": map[string]any{root: map[string]any{"grants": []permission.Grant{{Effect: "deny", Tool: "shell", Capabilities: []string{core.CapNetOutbound}}}}}})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
		}
		if !deny {
			if err := e.permStore.Allow("shell", tool.req); err != nil {
				t.Fatal(err)
			}
		}
		var events []PermissionEvent
		e.SetPermissionObserver(func(_ context.Context, event PermissionEvent) { events = append(events, event) })
		e.SetConfirmFn(func(protocol.ConfirmRequest) protocol.ConfirmReply {
			t.Error("remembered/denied capability should not ask")
			return protocol.Deny()
		})
		result := e.ExecuteBatch(t.Context(), []core.ToolCallItem{{ID: "remember", Name: "shell", Args: map[string]any{"command": "pwd"}}})[0]
		if deny {
			if result.Error == "" || len(events) != 1 || events[0].Args["source"] != "rule" || events[0].Args["approval"] == nil {
				t.Fatalf("denial=%+v result=%+v", events, result)
			}
		} else if result.Error != "" || len(events) != 0 {
			t.Fatalf("remembered=%+v result=%+v", events, result)
		}
	}
}

func TestPermissionObserverCancelledApprovalIsNotUserDenial(t *testing.T) {
	executor := newPermTestExecutor(t)
	reply := protocol.ConfirmReply{Cancelled: true}
	seen := []PermissionEvent{}
	executor.SetPermissionObserver(func(_ context.Context, event PermissionEvent) { seen = append(seen, event) })
	confirm := executor.observeConfirm(t.Context(), func(protocol.ConfirmRequest) protocol.ConfirmReply { return reply })
	confirm(protocol.ConfirmRequest{ToolName: "shell"})
	if len(seen) != 1 || seen[0].Denied {
		t.Fatalf("cancellation produced user denial: %+v", seen)
	}
}

func TestPermissionObserverExplicitDenialSurvivesCancellation(t *testing.T) {
	executor := newPermTestExecutor(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	denied := false
	executor.SetPermissionObserver(func(_ context.Context, event PermissionEvent) {
		if event.Denied {
			denied = event.Args["source"] == "user"
		}
	})
	confirm := executor.observeConfirm(ctx, func(protocol.ConfirmRequest) protocol.ConfirmReply { cancel(); return protocol.Deny() })
	confirm(protocol.ConfirmRequest{ToolName: "shell"})
	if !denied {
		t.Fatal("explicit user rejection lost when the run also cancels")
	}
}
