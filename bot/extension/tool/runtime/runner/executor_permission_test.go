package runner

import (
	"context"
	"nekocode/protocol"
	"strings"
	"sync"
	"testing"

	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/permission"
)

// fakeToolForPerm is a minimal tool for permission-engine integration tests.
type fakeToolForPerm struct {
	name string
}

func (t fakeToolForPerm) Name() string                                    { return t.name }
func (t fakeToolForPerm) Description() string                             { return "test" }
func (t fakeToolForPerm) Parameters() []core.Parameter                    { return nil }
func (t fakeToolForPerm) ExecutionMode(map[string]any) core.ExecutionMode { return core.ModeSequential }
func (t fakeToolForPerm) Execute(context.Context, map[string]any) (core.ToolOutput, error) {
	return core.ToolOutput{Text: "ran"}, nil
}

func newPermTestExecutor(t *testing.T) *Executor {
	t.Helper()
	e := newTestExecutor(fakeRegistry{
		"shell": fakeToolForPerm{name: "shell"},
		"write": fakeToolForPerm{name: "write"},
	})
	e.workspace.Configure("/repo", nil)
	e.SetPermissionPolicy(permission.PermissionsDecl{}, "/repo", "/home/user")
	return e
}

func TestPermEngine_DeniesSudo(t *testing.T) {
	e := newPermTestExecutor(t)
	e.SetConfirmFn(func(protocol.ConfirmRequest) protocol.ConfirmReply { return protocol.AllowOnce() })

	r := e.ExecuteBatch(context.Background(), []core.ToolCallItem{
		{ID: "1", Name: "shell", Args: map[string]any{"command": "sudo rm -rf /"}},
	})[0]
	if r.Error == "" {
		t.Fatal("expected sudo to be denied by builtin rule")
	}
}

func TestPermEngine_GitAddDoesNotMatchDdDeny(t *testing.T) {
	e := newPermTestExecutor(t)
	asked := false
	e.SetConfirmFn(func(protocol.ConfirmRequest) protocol.ConfirmReply {
		asked = true
		return protocol.AllowOnce()
	})

	r := e.ExecuteBatch(context.Background(), []core.ToolCallItem{
		{ID: "1", Name: "shell", Args: map[string]any{"command": "git add ."}},
	})[0]
	if r.Error != "" {
		t.Fatalf("git add should ask and run, got error: %v", r.Error)
	}
	if !asked {
		t.Fatal("git add should be an ask prompt, not a builtin deny")
	}
}

func TestPermEngine_DeniesDdCommand(t *testing.T) {
	e := newPermTestExecutor(t)
	e.SetConfirmFn(func(protocol.ConfirmRequest) protocol.ConfirmReply {
		t.Fatal("dd deny should not ask for confirmation")
		return protocol.AllowOnce()
	})

	r := e.ExecuteBatch(context.Background(), []core.ToolCallItem{
		{ID: "1", Name: "shell", Args: map[string]any{"command": "dd if=/dev/zero of=/tmp/x bs=1 count=1"}},
	})[0]
	if r.Error == "" {
		t.Fatal("dd should be denied by builtin rule")
	}
}

func TestPermEngine_AsksForUnrememberedShellCommand(t *testing.T) {
	e := newPermTestExecutor(t)
	asked := false
	e.SetConfirmFn(func(protocol.ConfirmRequest) protocol.ConfirmReply {
		asked = true
		return protocol.AllowOnce()
	})

	r := e.ExecuteBatch(context.Background(), []core.ToolCallItem{
		{ID: "1", Name: "shell", Args: map[string]any{"command": "go test ./..."}},
	})[0]
	if r.Error != "" {
		t.Fatalf("approved shell command should run, got error: %v", r.Error)
	}
	if !asked {
		t.Fatal("unremembered shell command should ask")
	}
}

func TestPermEngine_AsksForRm(t *testing.T) {
	e := newPermTestExecutor(t)
	asked := false
	e.SetConfirmFn(func(req protocol.ConfirmRequest) protocol.ConfirmReply {
		asked = true
		return protocol.AllowOnce()
	})

	r := e.ExecuteBatch(context.Background(), []core.ToolCallItem{
		{ID: "1", Name: "shell", Args: map[string]any{"command": "rm -rf /tmp/x"}},
	})[0]
	if !asked {
		t.Fatal("rm should trigger an ask prompt (builtin ask rule)")
	}
	if r.Error != "" {
		t.Fatalf("approved rm should run, got error: %v", r.Error)
	}
}

func TestPermEngine_AskDeniedCancels(t *testing.T) {
	e := newPermTestExecutor(t)
	e.SetConfirmFn(func(protocol.ConfirmRequest) protocol.ConfirmReply { return protocol.Deny() })

	r := e.ExecuteBatch(context.Background(), []core.ToolCallItem{
		{ID: "1", Name: "shell", Args: map[string]any{"command": "rm -rf /tmp/x"}},
	})[0]
	if r.Error == "" {
		t.Fatal("denied rm should return cancelled error")
	}
	// The denial must read as an explicit user decision. A bare "cancelled"
	// looks transient and made the model retry the same call — re-popping
	// the approval dialog the user had just dismissed.
	if !strings.Contains(r.Error, "denied by user") || !strings.Contains(r.Error, "do not retry") {
		t.Fatalf("denial feedback = %q, want explicit user-decision semantics", r.Error)
	}
}

func TestPermEngine_WriteToolNoPrompt(t *testing.T) {
	e := newPermTestExecutor(t)
	called := false
	e.SetConfirmFn(func(protocol.ConfirmRequest) protocol.ConfirmReply {
		called = true
		return protocol.AllowOnce()
	})

	r := e.ExecuteBatch(context.Background(), []core.ToolCallItem{
		{ID: "1", Name: "write", Args: map[string]any{"path": "/repo/src/a.go", "content": "x"}},
	})[0]
	if r.Error != "" {
		t.Fatalf("workspace write should be allowed without prompt, got: %v", r.Error)
	}
	if called {
		t.Fatal("write in workspace should not prompt (builtin allow)")
	}
}

func TestPermEngine_DeclaredDenyOverridesBuiltinAllow(t *testing.T) {
	e := newTestExecutor(fakeRegistry{
		"shell": fakeToolForPerm{name: "shell"},
	})
	// User declares: deny Bash(git push *). Builtin has no git-push rule, so
	// without the deny it would fall to default (ask). The deny must block it.
	e.SetPermissionPolicy(permission.PermissionsDecl{
		Deny: []string{"Bash(git push *)"},
	}, "/repo", "/home/user")
	e.SetConfirmFn(func(protocol.ConfirmRequest) protocol.ConfirmReply { return protocol.AllowOnce() })

	r := e.ExecuteBatch(context.Background(), []core.ToolCallItem{
		{ID: "1", Name: "shell", Args: map[string]any{"command": "git push origin main"}},
	})[0]
	if r.Error == "" {
		t.Fatal("declared deny should block git push")
	}
}

func TestPermEngine_RememberedAllowSkipsFuturePrompt(t *testing.T) {
	dir := t.TempDir()
	store := permission.NewStore(dir + "/perms.json")
	e := newTestExecutor(fakeRegistry{
		"shell": fakeToolForPerm{name: "shell"},
	})
	e.SetPermissionStore(store)
	e.SetPermissionPolicy(permission.PermissionsDecl{}, "/repo", "/home/user")

	var mu sync.Mutex
	askCount := 0
	e.SetConfirmFn(func(req protocol.ConfirmRequest) protocol.ConfirmReply {
		mu.Lock()
		askCount++
		mu.Unlock()
		return protocol.AllowRemembered()
	})

	// First rm call: asks, user allows + remember → rule persisted.
	e.ExecuteBatch(context.Background(), []core.ToolCallItem{
		{ID: "1", Name: "shell", Args: map[string]any{"command": "rm -rf /tmp/x"}},
	})

	// Same rm call: exact remembered allow rule should skip the prompt.
	e.ExecuteBatch(context.Background(), []core.ToolCallItem{
		{ID: "2", Name: "shell", Args: map[string]any{"command": "rm -rf /tmp/x"}},
	})

	mu.Lock()
	if askCount != 1 {
		t.Fatalf("expected 1 prompt for repeated exact command, got %d", askCount)
	}
	mu.Unlock()

	// A different rm target is covered by the remembered command-level rule.
	e.ExecuteBatch(context.Background(), []core.ToolCallItem{
		{ID: "3", Name: "shell", Args: map[string]any{"command": "rm -rf /tmp/y"}},
	})
	mu.Lock()
	defer mu.Unlock()
	if askCount != 1 {
		t.Fatalf("remembered command-level rule should cover the same command with different args, got %d prompts", askCount)
	}
}

func TestPermEngine_RememberedCompoundBashSkipsFuturePrompt(t *testing.T) {
	dir := t.TempDir()
	store := permission.NewStore(dir + "/perms.json")
	e := newTestExecutor(fakeRegistry{
		"shell": fakeToolForPerm{name: "shell"},
	})
	e.SetPermissionStore(store)
	e.SetPermissionPolicy(permission.PermissionsDecl{}, "/repo", "/home/user")

	askCount := 0
	e.SetConfirmFn(func(req protocol.ConfirmRequest) protocol.ConfirmReply {
		askCount++
		return protocol.AllowRemembered()
	})

	cmd := `echo "喵~ 你好！" && date && uname -a`
	e.ExecuteBatch(context.Background(), []core.ToolCallItem{
		{ID: "1", Name: "shell", Args: map[string]any{"command": cmd}},
	})
	e.ExecuteBatch(context.Background(), []core.ToolCallItem{
		{ID: "2", Name: "shell", Args: map[string]any{"command": cmd}},
	})

	if askCount != 1 {
		t.Fatalf("expected repeated compound command to prompt once, got %d", askCount)
	}
}

func TestPermEngine_RememberedDynamicBashStaysExact(t *testing.T) {
	dir := t.TempDir()
	store := permission.NewStore(dir + "/perms.json")
	e := newTestExecutor(fakeRegistry{
		"shell": fakeToolForPerm{name: "shell"},
	})
	e.SetPermissionStore(store)
	e.SetPermissionPolicy(permission.PermissionsDecl{}, "/repo", "/home/user")

	askCount := 0
	e.SetConfirmFn(func(req protocol.ConfirmRequest) protocol.ConfirmReply {
		askCount++
		return protocol.AllowRemembered()
	})

	e.ExecuteBatch(context.Background(), []core.ToolCallItem{
		{ID: "1", Name: "shell", Args: map[string]any{"command": `echo "喵~ 第一次！当前目录: $(pwd)" && date`}},
	})
	e.ExecuteBatch(context.Background(), []core.ToolCallItem{
		{ID: "2", Name: "shell", Args: map[string]any{"command": `echo "喵~ 第二次！当前目录: $(pwd)" && date`}},
	})

	if askCount != 2 {
		t.Fatalf("changed dynamic command must require a fresh approval, got %d prompts", askCount)
	}
}

func TestPermEngine_BroadAllowCannotApproveDynamicBash(t *testing.T) {
	dir := t.TempDir()
	store := permission.NewStore(dir + "/perms.json")
	e := newTestExecutor(fakeRegistry{"shell": fakeToolForPerm{name: "shell"}})
	e.SetPermissionStore(store)
	e.SetPermissionPolicy(permission.PermissionsDecl{Allow: []string{"Bash(*)"}}, "/repo", "/home/user")

	askCount := 0
	e.SetConfirmFn(func(protocol.ConfirmRequest) protocol.ConfirmReply {
		askCount++
		return protocol.AllowRemembered()
	})
	call := core.ToolCallItem{ID: "1", Name: "shell", Args: map[string]any{"command": `echo "$(date)"`}}
	e.ExecuteBatch(context.Background(), []core.ToolCallItem{call})
	call.ID = "2"
	e.ExecuteBatch(context.Background(), []core.ToolCallItem{call})

	if askCount != 1 {
		t.Fatalf("broad allow must prompt once before exact literal is remembered, got %d prompts", askCount)
	}
}

func TestCapabilityGrantDoesNotSuppressDynamicShellApproval(t *testing.T) {
	dir := t.TempDir()
	store := permission.NewStore(dir + "/perms.json")
	request := core.PermissionRequest{
		Capabilities: []string{core.CapNetOutbound},
		Scope:        "project",
		Details:      map[string]any{"workspace": "/repo"},
	}
	if err := store.Allow("shell", request); err != nil {
		t.Fatalf("allow capability: %v", err)
	}
	e := newTestExecutor(fakeRegistry{"shell": fakeToolForPerm{name: "shell"}})
	e.SetPermissionStore(store)
	e.SetPermissionPolicy(permission.PermissionsDecl{}, "/repo", "/home/user")

	decision := e.permissionDecisionForRule(permission.Decision{
		Effect: permission.EffectAsk,
		Assessment: permission.CallAssessment{
			Reason:  "dynamic shell execution",
			Signals: []string{string(permission.ShellCommandSubstitution)},
		},
	}, core.ToolCallItem{Name: "shell"}, &request)
	if !decision.prompt || decision.approval == nil {
		t.Fatalf("capability grant suppressed dynamic approval: %+v", decision)
	}
}

func TestPermEngine_UnrememberedSubcommandInCompoundAsks(t *testing.T) {
	dir := t.TempDir()
	store := permission.NewStore(dir + "/perms.json")
	for _, spec := range []string{"echo *", "date", "uname *"} {
		if err := store.RememberRule("/repo", permission.Rule{Tool: "bash", Specifier: spec, Effect: permission.EffectAllow}); err != nil {
			t.Fatalf("RememberRule(%q): %v", spec, err)
		}
	}
	e := newTestExecutor(fakeRegistry{
		"shell": fakeToolForPerm{name: "shell"},
	})
	e.SetPermissionStore(store)
	e.SetPermissionPolicy(permission.PermissionsDecl{}, "/repo", "/home/user")

	asked := false
	e.SetConfirmFn(func(req protocol.ConfirmRequest) protocol.ConfirmReply {
		asked = true
		return protocol.AllowOnce()
	})

	cmd := `echo "=== 测试 ===" && go test ./... && echo "" && echo "=== 项目模块 ===" && head -5 go.mod && echo "" && echo "=== 最近 git 日志 ===" && git log --oneline -5`
	e.ExecuteBatch(context.Background(), []core.ToolCallItem{
		{ID: "1", Name: "shell", Args: map[string]any{"command": cmd}},
	})
	if !asked {
		t.Fatal("compound shell command with unremembered go/head/git subcommands should ask")
	}
}
