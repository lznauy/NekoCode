package runner

import (
	"context"
	"fmt"
	"nekocode/protocol"
	"path/filepath"
	"strings"
	"testing"

	tools "nekocode/bot/extension/tool"
	builtinshell "nekocode/bot/extension/tool/builtin/shell"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/permission"
	"nekocode/bot/extension/tool/runtime/workspace"
)

type fakeRegistry map[string]core.Tool

func stringSliceArg(args map[string]any, key string) []string {
	switch values := args[key].(type) {
	case []string:
		return values
	case []any:
		out := make([]string, 0, len(values))
		for _, value := range values {
			if text, ok := value.(string); ok {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}

func newTestRegistry(items fakeRegistry) *tools.Registry {
	r := tools.New()
	for _, item := range items {
		options := tools.RegistrationOptions{}
		if privileged, ok := item.(interface {
			ExecuteWithPermission(context.Context, map[string]any, core.PermissionRequest) (string, error)
		}); ok {
			options.Privileged = privileged.ExecuteWithPermission
			if item.Name() == "shell" {
				options.PermissionPlan = (&builtinshell.ShellTool{}).PermissionPlan
			}
		}
		r.RegisterWithOptions(item, options)
	}
	return r
}

func newTestExecutor(items fakeRegistry) *Executor {
	return NewExecutor(newTestRegistry(items))
}

type fakeTool struct {
	name   string
	mode   core.ExecutionMode
	output string
}

func (t fakeTool) Name() string                                    { return t.name }
func (t fakeTool) Description() string                             { return "test" }
func (t fakeTool) Parameters() []core.Parameter                    { return nil }
func (t fakeTool) ExecutionMode(map[string]any) core.ExecutionMode { return t.mode }
func (t fakeTool) Execute(context.Context, map[string]any) (string, error) {
	if t.output != "" {
		return t.output, nil
	}
	return "ok", nil
}

type captureTool struct {
	fakeTool
	seen map[string]any
}

type privilegedCaptureTool struct {
	fakeTool
}

func (t privilegedCaptureTool) ExecuteWithPermission(context.Context, map[string]any, core.PermissionRequest) (string, error) {
	return "ok", nil
}

func (t *captureTool) Execute(_ context.Context, args map[string]any) (string, error) {
	t.seen = args
	return "ok", nil
}

func TestExecutorBatchPreservesCallOrderAcrossModes(t *testing.T) {
	e := newTestExecutor(fakeRegistry{
		"read":  fakeTool{name: "read", mode: core.ModeParallel},
		"write": fakeTool{name: "write", mode: core.ModeSequential},
	})

	results := e.ExecuteBatch(context.Background(), []core.ToolCallItem{
		{ID: "1", Name: "write", Args: map[string]any{"path": "a.go"}},
		{ID: "2", Name: "read", Args: map[string]any{"path": "a.go"}},
		{ID: "3", Name: "write", Args: map[string]any{"path": "b.go"}},
	})

	for i, wantID := range []string{"1", "2", "3"} {
		if results[i].ID != wantID {
			t.Fatalf("result %d has ID %q, want %q; results=%+v", i, results[i].ID, wantID, results)
		}
	}
}

func TestExecutorUsesRegistryWorkspaceAndPlanPolicy(t *testing.T) {
	manager := workspace.New(t.TempDir(), nil)
	registry := tools.New()
	registry.SetWorkspace(manager)
	registry.RegisterWithOptions(fakeTool{name: "read"}, tools.RegistrationOptions{PlanAllowed: true})
	e := NewExecutor(registry)
	if e.workspace != manager {
		t.Fatal("registry workspace was ignored")
	}
	entry, _ := registry.Lookup("read")
	if !e.planAllows("read", entry.PlanAllowed) || e.planAllows("write", false) {
		t.Fatal("registry plan policy was ignored")
	}
}

func TestExecutorUsesRegisteredPreviews(t *testing.T) {
	r := tools.New()
	r.RegisterWithOptions(fakeTool{name: "first"}, tools.RegistrationOptions{
		Preview: func(context.Context, map[string]any) string { return "first-preview" },
	})
	r.RegisterWithOptions(fakeTool{name: "second"}, tools.RegistrationOptions{
		Preview: func(context.Context, map[string]any) string { return "second-preview" },
	})
	e := NewExecutor(r)
	calls := []core.ToolCallItem{{Name: "first"}, {Name: "second"}}
	e.PreparePreviewsContext(context.Background(), calls)
	if calls[0].Args["_preview"] != "first-preview" || calls[1].Args["_preview"] != "second-preview" {
		t.Fatalf("previews = %#v, %#v", calls[0].Args, calls[1].Args)
	}
}

func TestPermissionPlanRunsOnlyAfterWorkspaceAccessIsApproved(t *testing.T) {
	planned := false
	registry := tools.New()
	registry.SetWorkspace(workspace.New(t.TempDir(), nil))
	tool := privilegedCaptureTool{fakeTool: fakeTool{name: "image_understand"}}
	registry.RegisterWithOptions(tool, tools.RegistrationOptions{
		Privileged: tool.ExecuteWithPermission,
		PermissionPlan: func(map[string]any, string) *core.PermissionRequest {
			planned = true
			return &core.PermissionRequest{Capabilities: []string{core.CapNetOutbound}}
		},
	})
	e := NewExecutor(registry)
	result := e.ExecuteBatch(context.Background(), []core.ToolCallItem{{
		ID: "image", Name: "image_understand", Args: map[string]any{"path": filepath.Join(t.TempDir(), "outside.png")},
	}})[0]
	if result.Error == "" {
		t.Fatal("expected workspace access rejection")
	}
	if planned {
		t.Fatal("permission planner ran before workspace approval")
	}
}

func TestDeniedFileToolDoesNotResolveOrPromptForWorkspacePath(t *testing.T) {
	registry := tools.New()
	registry.SetWorkspace(workspace.New(t.TempDir(), nil))
	registry.Register(fakeTool{name: "read", mode: core.ModeParallel})
	e := NewExecutor(registry)
	e.SetPermissionPolicy(permission.PermissionsDecl{Deny: []string{"read"}}, "/repo", "/home/user")
	prompted := false
	e.SetConfirmFn(func(protocol.ConfirmRequest) protocol.ConfirmReply {
		prompted = true
		return protocol.Deny()
	})

	result := e.ExecuteBatch(context.Background(), []core.ToolCallItem{{
		ID: "denied-read", Name: "read", Args: map[string]any{"path": filepath.Join(t.TempDir(), "outside.txt")},
	}})[0]
	if result.Error == "" || !strings.Contains(result.Error, "denied") {
		t.Fatalf("denied read result = %+v", result)
	}
	if prompted {
		t.Fatal("denied read prompted for workspace access before its deny rule")
	}
}

func TestExecutorBlocksPermissionDenyAndPlanMode(t *testing.T) {
	e := newTestExecutor(fakeRegistry{
		"blocked": fakeTool{name: "blocked", mode: core.ModeParallel},
		"writer":  fakeTool{name: "writer", mode: core.ModeSequential},
		"read":    fakeTool{name: "read", mode: core.ModeParallel},
	})
	e.SetPermissionPolicy(permission.PermissionsDecl{Deny: []string{"blocked"}}, "/repo", "/home/user")

	if got := e.ExecuteBatch(context.Background(), []core.ToolCallItem{{ID: "1", Name: "blocked"}})[0]; got.Error == "" {
		t.Fatal("expected permission deny error")
	}

	e.SetPlanMode(true)
	e.SetPlanTools("read")
	if got := e.ExecuteBatch(context.Background(), []core.ToolCallItem{{ID: "2", Name: "writer"}})[0]; got.Error == "" {
		t.Fatal("expected plan mode error")
	}
	if got := e.ExecuteBatch(context.Background(), []core.ToolCallItem{{ID: "3", Name: "read"}})[0]; got.Error != "" || got.Output != "ok" {
		t.Fatalf("plan mode should allow read-only exploration: %+v", got)
	}
}

func TestPlanModeToolAllowlist(t *testing.T) {
	e := newTestExecutor(fakeRegistry{})
	e.SetPlanTools("read", "grep", "glob", "list", "tree", "web_search", "web_fetch")
	for _, name := range []string{"read", "grep", "glob", "list", "tree", "web_search", "web_fetch"} {
		if !e.planAllows(name, false) {
			t.Errorf("%q should be allowed in plan mode", name)
		}
	}
	for _, name := range []string{"write", "edit", "shell", "process", "task", "todo_write", "index", "mcp__unknown"} {
		if e.planAllows(name, false) {
			t.Errorf("%q should be blocked in plan mode", name)
		}
	}
}

func TestHasPermissionDeclIncludesSandboxOnlyPolicy(t *testing.T) {
	if !hasPermissionDecl(permission.PermissionsDecl{
		Sandbox: map[string]permission.SandboxProfile{
			"Bash(git status *)": {SandboxMode: "read-only"},
		},
	}) {
		t.Fatal("sandbox-only permission policy must not be treated as empty")
	}
	if hasPermissionDecl(permission.PermissionsDecl{}) {
		t.Fatal("empty permission policy should be treated as empty")
	}
}

func TestExecutorProcessActionsDoNotPromptByDefault(t *testing.T) {
	e := newTestExecutor(fakeRegistry{
		"process": fakeTool{name: "process", mode: core.ModeParallel},
	})
	e.SetPermissionPolicy(permission.PermissionsDecl{}, "/repo", "/home/user")
	e.SetConfirmFn(func(protocol.ConfirmRequest) protocol.ConfirmReply {
		t.Fatal("process wait/watch/list should not prompt by default")
		return protocol.Deny()
	})

	got := e.ExecuteBatch(context.Background(), []core.ToolCallItem{{
		ID:   "1",
		Name: "process",
		Args: map[string]any{
			"action": "wait",
			"task":   "download",
		},
	}})[0]
	if got.Error != "" || got.Output != "ok" {
		t.Fatalf("unexpected result: %+v", got)
	}
}

func TestExecutorShellRunDoesNotDoublePromptAfterCommandAllow(t *testing.T) {
	e := newTestExecutor(fakeRegistry{
		"shell": fakeTool{name: "shell", mode: core.ModeSequential},
	})
	e.SetPermissionPolicy(permission.PermissionsDecl{}, "/repo", "/home/user")
	e.SetConfirmFn(func(protocol.ConfirmRequest) protocol.ConfirmReply {
		t.Fatal("shell run should not prompt again after command-level allow")
		return protocol.Deny()
	})

	got := e.ExecuteBatch(context.Background(), []core.ToolCallItem{{
		ID:   "1",
		Name: "shell",
		Args: map[string]any{
			"action":  "run",
			"command": "ls .",
		},
	}})[0]
	if got.Error != "" || got.Output != "ok" {
		t.Fatalf("unexpected result: %+v", got)
	}
}

func TestExecutorConfirmDenial(t *testing.T) {
	e := newTestExecutor(fakeRegistry{
		"writer": fakeTool{name: "writer", mode: core.ModeSequential},
	})
	e.SetPermissionPolicy(permission.PermissionsDecl{Ask: []string{"writer"}}, "/repo", "/home/user")
	e.SetConfirmFn(func(protocol.ConfirmRequest) protocol.ConfirmReply { return protocol.Deny() })

	got := e.ExecuteBatch(context.Background(), []core.ToolCallItem{{ID: "1", Name: "writer"}})[0]
	if got.Error == "" {
		t.Fatal("expected confirm denial")
	}
}

func TestExecutorWorkspacePromptAddsReadRoot(t *testing.T) {
	primary := t.TempDir()
	extra := t.TempDir()
	tool := &captureTool{fakeTool: fakeTool{name: "read", mode: core.ModeParallel}}
	e := newTestExecutor(fakeRegistry{"read": tool})
	e.workspace.Configure(primary, nil)

	prompts := 0
	e.SetConfirmFn(func(req protocol.ConfirmRequest) protocol.ConfirmReply {
		prompts++
		if req.ToolName != "workspace" {
			t.Fatalf("workspace access should prompt as workspace, got %+v", req)
		}
		if req.Args["access"] != string(workspace.AccessReadOnly) {
			t.Fatalf("read should request read-only access, got %+v", req.Args)
		}
		return protocol.AllowOnce()
	})

	target := filepath.Join(extra, "a.go")
	got := e.ExecuteBatch(context.Background(), []core.ToolCallItem{{
		ID: "1", Name: "read", Args: map[string]any{"path": target},
	}})[0]

	if got.Error != "" {
		t.Fatalf("read should run after workspace approval: %+v", got)
	}
	if prompts != 1 {
		t.Fatalf("expected one workspace prompt, got %d", prompts)
	}
	if tool.seen["path"] != target {
		t.Fatalf("tool should receive resolved target path, got %+v", tool.seen)
	}
}

func TestExecutorWorkspaceDenialBlocksBeforeExecute(t *testing.T) {
	primary := t.TempDir()
	extra := t.TempDir()
	tool := &captureTool{fakeTool: fakeTool{name: "write", mode: core.ModeSequential}}
	e := newTestExecutor(fakeRegistry{"write": tool})
	e.workspace.Configure(primary, nil)
	e.SetConfirmFn(func(protocol.ConfirmRequest) protocol.ConfirmReply { return protocol.Deny() })

	got := e.ExecuteBatch(context.Background(), []core.ToolCallItem{{
		ID: "1", Name: "write", Args: map[string]any{"path": filepath.Join(extra, "a.go")},
	}})[0]

	if got.Error == "" {
		t.Fatal("expected workspace denial to block write")
	}
	if tool.seen != nil {
		t.Fatalf("tool must not execute after workspace denial, saw %+v", tool.seen)
	}
}

func TestTruncateOutput(t *testing.T) {
	var output string
	for i := range maxLines + 5 {
		output += fmt.Sprintf("line %d\n", i)
	}
	got := truncateOutput(output)
	if len(got) >= len(output) {
		t.Fatal("expected truncated output")
	}
	if !strings.Contains(got, "truncated") {
		t.Fatalf("missing truncation marker: %q", got)
	}
	for _, want := range []string{"line 0", "line 49", "line 1955", "line 2004"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing retained line %q in %q", want, got)
		}
	}
	for _, notWant := range []string{"line 50\n", "line 1954\n"} {
		if strings.Contains(got, notWant) {
			t.Fatalf("middle line %q should be truncated from %q", notWant, got)
		}
	}
}

func TestExecutorPreservesTaskOutput(t *testing.T) {
	var output string
	for i := range maxLines + 5 {
		output += fmt.Sprintf("line %d\n", i)
	}
	e := newTestExecutor(fakeRegistry{
		"task": fakeTool{name: "task", mode: core.ModeParallel, output: output},
	})

	got := e.ExecuteBatch(context.Background(), []core.ToolCallItem{{ID: "1", Name: "task"}})[0]

	if got.Output != output {
		t.Fatal("task output should not be truncated")
	}
}

type permissionTool struct {
	fakeTool
	privilegedCalls int
	wrapError       bool
	req             core.PermissionRequest
}

func (t *permissionTool) Execute(context.Context, map[string]any) (string, error) {
	req := t.req
	if len(req.Capabilities) == 0 {
		req = core.PermissionRequest{
			Reason:       "needs network",
			Capabilities: []string{core.CapNetOutbound},
			Details: map[string]any{
				"workspace": "/repo",
			},
		}
	}
	err := testPermissionError{req: req}
	if t.wrapError {
		return "", fmt.Errorf("sandbox rejected command: %w", err)
	}
	return "", err
}

func (t *permissionTool) ExecuteWithPermission(context.Context, map[string]any, core.PermissionRequest) (string, error) {
	t.privilegedCalls++
	return "privileged ok", nil
}

type testPermissionError struct {
	req core.PermissionRequest
}

func (e testPermissionError) Error() string { return e.req.Reason }
func (e testPermissionError) PermissionRequest() core.PermissionRequest {
	return e.req
}

func TestExecutorUsesPersistedGrant(t *testing.T) {
	tool := &permissionTool{fakeTool: fakeTool{name: "shell", mode: core.ModeSequential}}
	store := permission.NewStore(filepath.Join(t.TempDir(), "permissions.json"))
	req := core.PermissionRequest{
		Capabilities: []string{core.CapNetOutbound},
		Details: map[string]any{
			"workspace": "/repo",
		},
	}
	if err := store.Allow("bash", req); err != nil {
		t.Fatalf("AllowProject: %v", err)
	}
	e := newTestExecutor(fakeRegistry{"shell": tool})
	e.SetPermissionStore(store)
	e.SetPermissionPolicy(permission.PermissionsDecl{Allow: []string{"Bash(*)"}}, "/repo", "/home/user")
	e.SetConfirmFn(func(protocol.ConfirmRequest) protocol.ConfirmReply {
		t.Fatal("confirm should not be called when a persisted grant matches")
		return protocol.Deny()
	})

	got := e.ExecuteBatch(context.Background(), []core.ToolCallItem{{
		ID:   "1",
		Name: "shell",
		Args: map[string]any{"command": "npm install", "network": true},
	}})[0]

	if got.Error != "" || got.Output != "privileged ok" {
		t.Fatalf("unexpected result: %+v", got)
	}
	if tool.privilegedCalls != 1 {
		t.Fatalf("privilegedCalls = %d, want 1", tool.privilegedCalls)
	}
}

func TestExecutorPermissionAllowOnceDoesNotPersistGrant(t *testing.T) {
	tool := &permissionTool{fakeTool: fakeTool{name: "shell", mode: core.ModeSequential}}
	store := permission.NewStore(filepath.Join(t.TempDir(), "permissions.json"))
	e := newTestExecutor(fakeRegistry{"shell": tool})
	e.SetPermissionStore(store)
	e.SetPermissionPolicy(permission.PermissionsDecl{Allow: []string{"Bash(*)"}}, "/repo", "/home/user")
	e.SetConfirmFn(func(protocol.ConfirmRequest) protocol.ConfirmReply {
		return protocol.AllowOnce()
	})

	got := e.ExecuteBatch(context.Background(), []core.ToolCallItem{{ID: "1", Name: "shell"}})[0]
	if got.Error != "" {
		t.Fatalf("unexpected error: %s", got.Error)
	}

	req := core.PermissionRequest{
		Capabilities: []string{core.CapNetOutbound},
		Details: map[string]any{
			"workspace": "/repo",
		},
	}
	if _, ok := store.Match("bash", req); ok {
		t.Fatal("allow-once should not persist grant")
	}
}

func TestExecutorPreApprovedEscalationOnceDoesNotPersistGrant(t *testing.T) {
	// A one-time unified approval is bound to this tool call. It must not be
	// written to disk or retained as a session-wide capability grant.
	tool := &permissionTool{fakeTool: fakeTool{name: "shell", mode: core.ModeSequential}}
	store := permission.NewStore(filepath.Join(t.TempDir(), "permissions.json"))
	e := newTestExecutor(fakeRegistry{"shell": tool})
	e.SetPermissionStore(store)
	e.SetPermissionPolicy(permission.PermissionsDecl{Ask: []string{"shell"}}, "/repo", "/home/user")
	e.SetConfirmFn(func(protocol.ConfirmRequest) protocol.ConfirmReply {
		return protocol.ConfirmReply{Allowed: true} // Remember: false
	})

	got := e.ExecuteBatch(context.Background(), []core.ToolCallItem{{ID: "1", Name: "shell"}})[0]
	if got.Error != "" || got.Output != "privileged ok" {
		t.Fatalf("unexpected result: %+v", got)
	}

	req := core.PermissionRequest{
		Capabilities: []string{core.CapNetOutbound},
		Details: map[string]any{
			"workspace": "/repo",
		},
	}
	// Disk store must NOT carry the one-time grant.
	if _, ok := store.Match("bash", req); ok {
		t.Fatal("one-time pre-approved escalation must NOT persist grant to disk")
	}
}

func TestUnifiedApprovalExecutesPrivilegedRetryInSameCall(t *testing.T) {
	tool := &permissionTool{fakeTool: fakeTool{name: "shell", mode: core.ModeSequential}}
	store := permission.NewStore(filepath.Join(t.TempDir(), "perm.json"))
	e := newTestExecutor(fakeRegistry{"shell": tool})
	e.SetPermissionStore(store)
	e.SetPermissionPolicy(permission.PermissionsDecl{Ask: []string{"shell"}}, "/repo", "/home/user")
	e.SetConfirmFn(func(protocol.ConfirmRequest) protocol.ConfirmReply {
		return protocol.ConfirmReply{Allowed: true}
	})

	first := e.ExecuteBatch(context.Background(), []core.ToolCallItem{{ID: "1", Name: "shell"}})[0]
	if first.Error != "" || first.Output != "privileged ok" {
		t.Fatalf("first call: %+v", first)
	}

}

func TestExecutorPreApprovedEscalationRememberPersistsGrant(t *testing.T) {
	tool := &permissionTool{fakeTool: fakeTool{name: "shell", mode: core.ModeSequential}}
	store := permission.NewStore(filepath.Join(t.TempDir(), "permissions.json"))
	e := newTestExecutor(fakeRegistry{"shell": tool})
	e.SetPermissionStore(store)
	e.SetPermissionPolicy(permission.PermissionsDecl{Ask: []string{"shell"}}, "/repo", "/home/user")
	e.SetConfirmFn(func(protocol.ConfirmRequest) protocol.ConfirmReply {
		return protocol.ConfirmReply{Allowed: true, Remember: true}
	})

	got := e.ExecuteBatch(context.Background(), []core.ToolCallItem{{ID: "1", Name: "shell"}})[0]
	if got.Error != "" || got.Output != "privileged ok" {
		t.Fatalf("unexpected result: %+v", got)
	}

	req := core.PermissionRequest{
		Capabilities: []string{core.CapNetOutbound},
		Details: map[string]any{
			"workspace": "/repo",
		},
	}
	if _, ok := store.Match("shell", req); !ok {
		t.Fatal("remembered pre-approved escalation should persist grant")
	}
}

func TestExecutorRecognizesWrappedPermissionError(t *testing.T) {
	tool := &permissionTool{
		fakeTool:  fakeTool{name: "shell", mode: core.ModeSequential},
		wrapError: true,
	}
	e := newTestExecutor(fakeRegistry{"shell": tool})
	e.SetPermissionPolicy(permission.PermissionsDecl{Allow: []string{"Bash(*)"}}, "/repo", "/home/user")
	e.SetConfirmFn(func(protocol.ConfirmRequest) protocol.ConfirmReply {
		return protocol.AllowOnce()
	})

	got := e.ExecuteBatch(context.Background(), []core.ToolCallItem{{ID: "1", Name: "shell"}})[0]
	if got.Error != "" || got.Output != "privileged ok" {
		t.Fatalf("unexpected result: %+v", got)
	}
	if tool.privilegedCalls != 1 {
		t.Fatalf("privilegedCalls = %d, want 1", tool.privilegedCalls)
	}
}

func TestExecutorPermissionRememberPersistsGrant(t *testing.T) {
	tool := &permissionTool{fakeTool: fakeTool{name: "shell", mode: core.ModeSequential}}
	store := permission.NewStore(filepath.Join(t.TempDir(), "permissions.json"))
	e := newTestExecutor(fakeRegistry{"shell": tool})
	e.SetPermissionStore(store)
	e.SetPermissionPolicy(permission.PermissionsDecl{Allow: []string{"Bash(*)"}}, "/repo", "/home/user")
	e.SetConfirmFn(func(protocol.ConfirmRequest) protocol.ConfirmReply {
		return protocol.AllowRemembered()
	})

	got := e.ExecuteBatch(context.Background(), []core.ToolCallItem{{ID: "1", Name: "shell"}})[0]
	if got.Error != "" {
		t.Fatalf("unexpected error: %s", got.Error)
	}

	req := core.PermissionRequest{
		Capabilities: []string{core.CapNetOutbound},
		Details: map[string]any{
			"workspace": "/repo",
		},
	}
	if _, ok := store.Match("shell", req); !ok {
		t.Fatal("remembered permission should persist grant")
	}
}

func TestExecutorPermissionErrorDoesNotTellShellToDeclareCapabilities(t *testing.T) {
	tool := &permissionTool{
		fakeTool: fakeTool{name: "shell", mode: core.ModeSequential},
		req: core.PermissionRequest{
			Reason:       "needs host",
			Capabilities: []string{core.CapProcessHost},
		},
	}
	e := newTestExecutor(fakeRegistry{"shell": tool})
	e.SetPermissionPolicy(permission.PermissionsDecl{Allow: []string{"Bash(*)"}}, "/repo", "/home/user")

	got := e.ExecuteBatch(context.Background(), []core.ToolCallItem{{ID: "1", Name: "shell", Args: map[string]any{"command": "npm run dev"}}})[0]

	if !strings.Contains(got.Error, "no approval available in this runtime") {
		t.Fatalf("error = %q, want a headless-style 'no approval available' message", got.Error)
	}
	if strings.Contains(got.Error, "Retry the shell call") || strings.Contains(got.Error, `capabilities ["process.host"]`) {
		t.Fatalf("error must not tell the model to request capabilities: %q", got.Error)
	}
}

func TestExecutorPermissionErrorExplainsMissingApprovalForDeclaredCapabilities(t *testing.T) {
	// The model already declared process.host but the runtime has no
	// confirmFn (headless). The model must be told the capability is not
	// approvable here — NOT "wait for the prompt" or "ask the user in
	// chat" (those used to leak the dialog's existence and made the model
	// reply in chat asking the user to click confirm).
	tool := &permissionTool{
		fakeTool: fakeTool{name: "shell", mode: core.ModeSequential},
		req: core.PermissionRequest{
			Reason:       "needs host",
			Capabilities: []string{core.CapProcessHost},
		},
	}
	e := newTestExecutor(fakeRegistry{"shell": tool})
	e.SetPermissionPolicy(permission.PermissionsDecl{Allow: []string{"Bash(*)"}}, "/repo", "/home/user")

	got := e.ExecuteBatch(context.Background(), []core.ToolCallItem{{
		ID:   "1",
		Name: "shell",
		Args: map[string]any{
			"command":      "npm run dev",
			"sandbox_mode": "host",
		},
	}})[0]

	if !strings.Contains(got.Error, "no approval available in this runtime") {
		t.Fatalf("error = %q, want a headless-style 'no approval available' message", got.Error)
	}
	if strings.Contains(got.Error, "wait for") || strings.Contains(got.Error, "ask the user") {
		t.Fatalf("error must not leak dialog instructions to the model: %q", got.Error)
	}
}

// TestExecutor_StaleEscalationTokenNotShared regresses bug #1: a pre-approval
// minted for one tool call leaked under the old key-by-tool-name
// scheme and was silently spent by an unrelated later call to the same tool —
// even a process.host escalation. Keying by ToolCallItem.ID plus a cleanup on
// the success path closes that path.
func TestExecutor_StaleEscalationTokenNotShared(t *testing.T) {
	tool := &stickyEscalationTool{fakeTool: fakeTool{name: "shell", mode: core.ModeSequential}}
	store := permission.NewStore(filepath.Join(t.TempDir(), "perm.json"))
	e := newTestExecutor(fakeRegistry{"shell": tool})
	e.SetPermissionStore(store)
	e.SetPermissionPolicy(permission.PermissionsDecl{Ask: []string{"shell"}}, "/repo", "/home/user")

	confirmCount := 0
	e.SetConfirmFn(func(req protocol.ConfirmRequest) protocol.ConfirmReply {
		// unified prompt for call A includes its predicted capability.
		// basic prompt for call B has no matching predicted capability.
		// any escalation dialog for call B: deny (so we expect a fresh dialog).
		confirmCount++
		switch confirmCount {
		case 1:
			return protocol.ConfirmReply{Allowed: true}
		case 2:
			return protocol.ConfirmReply{Allowed: true} // no escalation
		default:
			return protocol.Deny()
		}
	})

	// Call A: tool's Execute succeeds (no PermissionError), user clicked
	// The unified decision mints a pre-approval token that is never spent
	// because there is no escalation.
	tool.escalate = false
	tool.capability = core.CapNetOutbound
	callA := e.ExecuteBatch(context.Background(), []core.ToolCallItem{{
		ID: "A", Name: "shell",
		Args: map[string]any{"command": "ls", "network": true},
	}})[0]
	if callA.Error != "" || callA.Output != "ok" {
		t.Fatalf("call A: %+v", callA)
	}

	// Call B: tool now requests a capability. User picks "allow only" in the
	// basic dialog. Under the stale-token bug, the leftover from call A
	// would silently bring call B into ExecuteWithPermission (1 privileged
	// call, no escalation dialog). With the fix, the token is scoped to
	// call A's ID and cleared on A's success path, so call B falls through
	// to a fresh escalation dialog — which our confirmFn denies.
	tool.escalate = true
	tool.capability = core.CapProcessHost
	callB := e.ExecuteBatch(context.Background(), []core.ToolCallItem{{
		ID: "B", Name: "shell",
		Args: map[string]any{"command": "go get x", "network": true},
	}})[0]
	if callB.Error == "" {
		t.Fatalf("call B must fall through to an escalation dialog and be denied; "+
			"got success (privilegedCalls=%d). The leftover token from call A "+
			"was probably silently spent — bug #1 recurrence.", tool.privilegedCalls)
	}
	if tool.privilegedCalls != 0 {
		t.Fatalf("privilegedCalls = %d, want 0 (the denial must prevent ExecuteWithPermission)",
			tool.privilegedCalls)
	}
}

// stickyEscalationTool succeeds or throws a PermissionError based on
// `escalate`, and registers a privileged retry callback.
type stickyEscalationTool struct {
	fakeTool
	escalate        bool
	capability      string
	privilegedCalls int
}

func (t *stickyEscalationTool) Execute(context.Context, map[string]any) (string, error) {
	if !t.escalate {
		return "ok", nil
	}
	capability := t.capability
	if capability == "" {
		capability = core.CapNetOutbound
	}
	return "", testPermissionError{req: core.PermissionRequest{
		Reason:       "needs network",
		Capabilities: []string{capability},
		Details:      map[string]any{"workspace": "/repo"},
	}}
}

func (t *stickyEscalationTool) ExecuteWithPermission(context.Context, map[string]any, core.PermissionRequest) (string, error) {
	t.privilegedCalls++
	return "privileged ok", nil
}

// TestExecutor_SkipsBasicPromptWhenGrantMatches regresses bug #5: a shell run
// with an explicit sandbox opening used to re-prompt the basic "ask" dialog on
// every retry, even when the grant had already been issued and a session/disk
// grant matched. The fix pre-checks grant coverage and skips the basic prompt
// — but ONLY when the engine fell through to its default effect (no explicit
// rule matched). Explicit ask rules (rm *, declared Bash(), ...) always prompt.
//
// This test uses a command with NO matching builtin rule ("go get x") so the
// engine returns the default EffectAsk with dec.Rule.Tool == "" — the branch
// where the grant shortcut is allowed.
func TestExecutor_SkipsBasicPromptWhenGrantMatches(t *testing.T) {
	tool := &stickyEscalationTool{
		fakeTool: fakeTool{name: "shell", mode: core.ModeSequential},
		escalate: true,
	}
	store := permission.NewStore(filepath.Join(t.TempDir(), "perm.json"))
	e := newTestExecutor(fakeRegistry{"shell": tool})
	e.SetPermissionStore(store)
	// Empty policy: only builtin rules plus the default ask for shell commands.
	e.SetPermissionPolicy(permission.PermissionsDecl{}, "/repo", "/home/user")

	// Pre-populate the store with a project grant covering net.outbound
	// scoped to /repo (matches the store's project root).
	req := core.PermissionRequest{
		Capabilities: []string{core.CapNetOutbound},
		Details:      map[string]any{"workspace": "/repo"},
	}
	if err := store.Allow("bash", req); err != nil {
		t.Fatalf("seed grant: %v", err)
	}

	// confirmFn must NEVER fire — the basic dialog is skipped because the
	// predicted grant already covers the declared caps; the escalation path
	// then silently re-runs via ExecuteWithPermission.
	e.SetConfirmFn(func(protocol.ConfirmRequest) protocol.ConfirmReply {
		t.Fatal("confirm must not be called when a grant already matches the declaration")
		return protocol.Deny()
	})

	got := e.ExecuteBatch(context.Background(), []core.ToolCallItem{{
		ID:   "1",
		Name: "shell",
		Args: map[string]any{
			"command": "go get x",
			"network": true,
		},
	}})[0]
	if got.Error != "" || got.Output != "privileged ok" {
		t.Fatalf("unexpected result: %+v", got)
	}
	if tool.privilegedCalls != 1 {
		t.Fatalf("privilegedCalls = %d, want 1", tool.privilegedCalls)
	}
}

// TestExecutor_AskRuleStillPromptsDespiteGrant is the counterpart guard: a
// declared ask rule for `rm *` MUST still prompt the basic dialog, even if
// the user has a net.outbound capability grant for shell. Capability grant
// is orthogonal to command-level ask — bypassing the latter would let any
// prior capability approval silently whitelist dangerous commands.
func TestExecutor_AskRuleStillPromptsDespiteGrant(t *testing.T) {
	tool := &stickyEscalationTool{
		fakeTool: fakeTool{name: "shell", mode: core.ModeSequential},
		escalate: true,
	}
	store := permission.NewStore(filepath.Join(t.TempDir(), "perm.json"))
	e := newTestExecutor(fakeRegistry{"shell": tool})
	e.SetPermissionStore(store)
	// rm * has a builtin ask rule; combine with a net.outbound grant.
	req := core.PermissionRequest{
		Capabilities: []string{core.CapNetOutbound},
		Details:      map[string]any{"workspace": "/repo"},
	}
	if err := store.Allow("bash", req); err != nil {
		t.Fatalf("seed grant: %v", err)
	}
	e.SetPermissionPolicy(permission.PermissionsDecl{}, "/repo", "/home/user")

	calls := 0
	e.SetConfirmFn(func(protocol.ConfirmRequest) protocol.ConfirmReply {
		calls++
		// User denies the basic prompt — rm is dangerous.
		return protocol.Deny()
	})

	got := e.ExecuteBatch(context.Background(), []core.ToolCallItem{{
		ID:   "1",
		Name: "shell",
		Args: map[string]any{
			"command": "rm -rf /tmp/x",
			"network": true,
		},
	}})[0]
	if calls != 1 {
		t.Fatalf("basic prompt must fire for rm * despite capability grant; got %d calls", calls)
	}
	if got.Error == "" {
		t.Fatalf("expected the basic dialog denial to block the call; got %+v", got)
	}
	if tool.privilegedCalls != 0 {
		t.Fatalf("privilegedCalls = %d, want 0 (denial must not run privileged path)",
			tool.privilegedCalls)
	}
}

// TestExecutor_PermissionDeniedByUserDoesNotLeakDialogToModel regresses the
// user-reported bug: when shell requested an explicit sandbox opening and the
// user dismissed the confirm dialog, the old permissionFailureMessage told
// the model "Wait for and answer the permission prompt; if no prompt appears,
// this runtime has no confirmation callback...". The model read that as
// "I should tell the user to click confirm in the UI" and replied in chat
// asking the user to approve — even though the dialog had already been
// shown and dismissed. The fix gives the model a plain "permission denied
// by user" error with no dialog-leakage.
func TestExecutor_PermissionDeniedByUserDoesNotLeakDialogToModel(t *testing.T) {
	tool := &stickyEscalationTool{
		fakeTool: fakeTool{name: "shell", mode: core.ModeSequential},
		escalate: true,
	}
	store := permission.NewStore(filepath.Join(t.TempDir(), "perm.json"))
	e := newTestExecutor(fakeRegistry{"shell": tool})
	e.SetPermissionStore(store)
	e.SetPermissionPolicy(permission.PermissionsDecl{Ask: []string{"shell"}}, "/repo", "/home/user")

	// Basic dialog: user allows (so we get to the escalation dialog).
	// Escalation dialog: user denies.
	promptIdx := 0
	e.SetConfirmFn(func(req protocol.ConfirmRequest) protocol.ConfirmReply {
		promptIdx++
		// The basic prompt has no approval context; the escalation prompt does.
		if req.Approval != nil {
			return protocol.Deny() // user dismissed the escalation dialog
		}
		return protocol.AllowOnce() // basic prompt: allow
	})

	got := e.ExecuteBatch(context.Background(), []core.ToolCallItem{{
		ID:   "1",
		Name: "shell",
		Args: map[string]any{
			"command": "npm run dev",
		},
	}})[0]

	if got.Error == "" {
		t.Fatalf("expected the call to fail since the user denied the escalation; got %+v", got)
	}
	// Must NOT contain the old dialog-leaking instructions.
	for _, banned := range []string{"Wait for and answer", "ask the user", "no confirmation callback"} {
		if strings.Contains(got.Error, banned) {
			t.Fatalf("error leaks dialog instructions to model (%q): %q", banned, got.Error)
		}
	}
	// Must tell the model the user denied.
	if !strings.Contains(got.Error, "denied by user") {
		t.Fatalf("error should say 'denied by user'; got %q", got.Error)
	}
}

func TestExecutorShellRunPromptsCommandThenNetwork(t *testing.T) {
	tool := &fakeShellNetworkTool{fakeTool: fakeTool{name: "shell", mode: core.ModeSequential}}
	store := permission.NewStore(filepath.Join(t.TempDir(), "perm.json"))
	e := newTestExecutor(fakeRegistry{"shell": tool})
	e.SetPermissionStore(store)
	e.SetPermissionPolicy(permission.PermissionsDecl{}, "/repo", "/home/user")

	var prompts []protocol.ConfirmRequest
	e.SetConfirmFn(func(req protocol.ConfirmRequest) protocol.ConfirmReply {
		prompts = append(prompts, req)
		return protocol.AllowOnce()
	})

	got := e.ExecuteBatch(context.Background(), []core.ToolCallItem{{
		ID:   "1",
		Name: "shell",
		Args: map[string]any{
			"action":  "run",
			"command": "npm run dev",
		},
	}})[0]

	if got.Error != "" || got.Output != "started with network" {
		t.Fatalf("unexpected result: %+v", got)
	}
	if len(prompts) != 2 {
		t.Fatalf("expected command prompt then network prompt, got %d: %+v", len(prompts), prompts)
	}
	if prompts[0].Approval != nil {
		t.Fatalf("first prompt must be command approval, got %+v", prompts[0])
	}
	if prompts[0].ToolName != "shell" {
		t.Fatalf("first prompt should display shell command approval, got %+v", prompts[0])
	}
	if prompts[0].Args["command"] != "npm run dev" {
		t.Fatalf("first prompt should show actual command, got %+v", prompts[0].Args)
	}
	if prompts[0].Approval != nil && prompts[0].Approval.Combined {
		t.Fatalf("first prompt must not claim an unpredicted capability, got %+v", prompts[0])
	}
	if prompts[1].ToolName != "shell" {
		t.Fatalf("second prompt should remain shell capability approval, got %+v", prompts[1])
	}
	if prompts[1].Approval == nil || !strings.Contains(prompts[1].Approval.Reason, "local network") {
		t.Fatalf("second prompt must be network capability approval, got %+v", prompts[1])
	}
	if tool.privilegedCalls != 1 {
		t.Fatalf("privilegedCalls = %d, want 1", tool.privilegedCalls)
	}
}

// TestExecutor_PermissionRetryErrorShowsRealRuntimeFailure regresses the
// user-reported bug where the LLM declared process.host, the escalation path
// ran (user clicked "允许") and ExecuteWithPermission then failed — yet the
// model was shown the *original* PermissionError ("permission required:
// command requests unsandboxed host execution"). The "permission required"
// wording made the model misread it as "I should ask the user to authorize"
// and reply in chat asking the user to click the confirm button, even though
// escalation had already happened. With the fix, failRetryError surfaces the
// actual ExecuteWithPermission runtime error (wrapped so the model knows it
// is a runtime failure, not a request for authorization).
func TestExecutor_PermissionRetryErrorShowsRealRuntimeFailure(t *testing.T) {
	// privilegedExecuteErrTool: ExecuteWithPermission returns a sandbox
	// backend "unavailable" error, mimicking the user's environment where
	// the OS sandbox can't actually host-execute the command.
	tool := &privilegedExecuteErrTool{
		fakeTool: fakeTool{name: "shell", mode: core.ModeSequential},
		retryErr: fmt.Errorf("sandbox backend unavailable: this OS cannot run host processes"),
	}
	store := permission.NewStore(filepath.Join(t.TempDir(), "perm.json"))
	e := newTestExecutor(fakeRegistry{"shell": tool})
	e.SetPermissionStore(store)
	e.SetPermissionPolicy(permission.PermissionsDecl{}, "/repo", "/home/user")

	// Basic dialog: user allows; this test exercises the fallback escalation
	// dialog because the capability cannot be predicted in advance.
	e.SetConfirmFn(func(req protocol.ConfirmRequest) protocol.ConfirmReply {
		if req.Approval != nil {
			return protocol.AllowOnce() // escalation dialog → user clicks 允许
		}
		return protocol.AllowOnce() // basic dialog → allow
	})

	got := e.ExecuteBatch(context.Background(), []core.ToolCallItem{{
		ID:   "1",
		Name: "shell",
		Args: map[string]any{
			"command":      "npm run dev",
			"sandbox_mode": "host",
		},
	}})[0]

	if got.Error == "" {
		t.Fatalf("expected failure; got %+v", got)
	}
	// MUST NOT contain the misleading "permission required" wording from
	// the original PermissionError.
	for _, banned := range []string{"permission required", "requests unsandboxed host"} {
		if strings.Contains(got.Error, banned) {
			t.Fatalf("error must not leak original permission-request text (%q): %q",
				banned, got.Error)
		}
	}
	// MUST contain the real runtime error text so the model reacts to the
	// backend failure itself.
	if !strings.Contains(got.Error, "sandbox backend unavailable") {
		t.Fatalf("error should surface the real runtime failure; got %q", got.Error)
	}
	// And lead-in that makes clear this is a runtime failure, not a
	// waiting-for-authorization request.
	if !strings.Contains(got.Error, "sandbox/privileged execution failed") {
		t.Fatalf("error should make clear this is a runtime failure; got %q", got.Error)
	}
}

func TestMergeSandboxArgsPreservesExplicitArgs(t *testing.T) {
	got := mergeSandboxArgs(map[string]any{
		"command":        "git status",
		"sandbox_mode":   "workspace-write",
		"network":        false,
		"writable_roots": []any{"/explicit"},
	}, permission.SandboxProfile{
		SandboxMode:   "read-only",
		Network:       true,
		WritableRoots: []string{"/profile"},
	})

	if got["sandbox_mode"] != "workspace-write" {
		t.Fatalf("explicit sandbox_mode should win, got %+v", got)
	}
	// An explicit network=false from the caller must be respected — builtin
	// rules are defaults, not overrides.
	if got["network"] != false {
		t.Fatalf("explicit network=false should be preserved, got %+v", got)
	}
	roots := stringSliceArg(got, "writable_roots")
	if len(roots) != 1 || roots[0] != "/explicit" {
		t.Fatalf("explicit writable_roots should win, got %+v", got)
	}
}

func TestApplySandboxProfileToShellRun(t *testing.T) {
	e := newTestExecutor(fakeRegistry{})
	e.SetPermissionPolicy(permission.PermissionsDecl{
		Sandbox: map[string]permission.SandboxProfile{
			"Bash(npm run dev)": {
				SandboxMode:   "workspace-write",
				Network:       true,
				WritableRoots: []string{"/home/user/.npm"},
			},
		},
	}, "/repo", "/home/user")

	got := e.applySandboxProfile(core.ToolCallItem{
		ID:   "1",
		Name: "shell",
		Args: map[string]any{
			"action":  "run",
			"command": "npm run dev",
		},
	})

	if got.Args["sandbox_mode"] != "workspace-write" || got.Args["network"] != true {
		t.Fatalf("sandbox profile not applied: %+v", got.Args)
	}
	roots := stringSliceArg(got.Args, "writable_roots")
	if len(roots) != 1 || roots[0] != "/home/user/.npm" {
		t.Fatalf("writable roots not applied: %+v", got.Args)
	}
}

func TestApplyBuiltinSandboxProfileForPackageScaffold(t *testing.T) {
	e := newTestExecutor(fakeRegistry{})
	e.SetPermissionPolicy(permission.PermissionsDecl{}, "/repo", "/home/user")

	shellCall := e.applySandboxProfile(core.ToolCallItem{
		ID:   "1",
		Name: "shell",
		Args: map[string]any{
			"action":  "run",
			"command": "pnpm create vite work-planner --template react",
		},
	})

	if shellCall.Args["network"] != true {
		t.Fatalf("shell pnpm create vite should request network by default: %+v", shellCall.Args)
	}
}

// privilegedExecuteErrTool succeeds (no, throws PermissionError), then
// ExecuteWithPermission returns retryErr (real runtime error), simulating
// e.g. backend.RunHost failing in the user's environment.
type privilegedExecuteErrTool struct {
	fakeTool
	retryErr error
}

func (t *privilegedExecuteErrTool) Execute(context.Context, map[string]any) (string, error) {
	return "", testPermissionError{req: core.PermissionRequest{
		Reason:       "command requests unsandboxed host execution",
		Capabilities: []string{core.CapProcessHost},
		Details:      map[string]any{"workspace": "/repo"},
	}}
}

func (t *privilegedExecuteErrTool) ExecuteWithPermission(context.Context, map[string]any, core.PermissionRequest) (string, error) {
	return "", t.retryErr
}

type fakeShellNetworkTool struct {
	fakeTool
	privilegedCalls int
}

func (t *fakeShellNetworkTool) Execute(context.Context, map[string]any) (string, error) {
	return "", testPermissionError{req: core.PermissionRequest{
		Reason:       "shell command needs local network listener access",
		Capabilities: []string{core.CapNetOutbound},
		Scope:        "project",
		Details:      map[string]any{"workspace": "/repo"},
	}}
}

func (t *fakeShellNetworkTool) ExecuteWithPermission(_ context.Context, _ map[string]any, req core.PermissionRequest) (string, error) {
	t.privilegedCalls++
	if len(req.Capabilities) != 1 || req.Capabilities[0] != core.CapNetOutbound {
		return "", fmt.Errorf("unexpected grant: %+v", req)
	}
	return "started with network", nil
}
