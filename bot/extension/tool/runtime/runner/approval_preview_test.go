package runner

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/permission"
	"nekocode/protocol"
)

func TestParallelRememberKeepsAllGrantsInActiveEngine(t *testing.T) {
	e := newPermTestExecutor(t)
	root := t.TempDir()
	e.SetProjectStore(root)
	e.SetPermissionPolicy(permission.PermissionsDecl{}, root, root)
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs <- e.rememberAllowRule("shell", map[string]any{"command": fmt.Sprintf("command%d --flag", i)})
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 12; i++ {
		call := core.ToolCallItem{Name: "shell", Args: map[string]any{"command": fmt.Sprintf("command%d --another-flag", i)}}
		if d := e.evaluatePermission(t.Context(), call, nil); d.prompt || d.block {
			t.Fatalf("concurrent grant %d absent from active engine: %+v", i, d)
		}
	}
}

func TestOnceCapabilityCannotBeRememberedByReply(t *testing.T) {
	for _, preapproved := range []bool{false, true} {
		e := newPermTestExecutor(t)
		root := t.TempDir()
		e.SetProjectStore(root)
		req := core.PermissionRequest{Scope: "once", Capabilities: []string{core.CapNetOutbound}, Details: map[string]any{"workspace": root}}
		tool := &permissionTool{}
		_, ok, reason, cause := e.tryPermissionEscalation(t.Context(), tool.ExecuteWithPermission, core.ToolCallItem{Name: "shell"}, testPermissionError{req: req}, func(protocol.ConfirmRequest) protocol.ConfirmReply { return protocol.AllowRemembered() }, escalationApproval{remember: true, predicted: &req}, preapproved, false)
		if !ok || tool.privilegedCalls != 1 {
			t.Fatalf("once approval should execute once: %t, %v, %v", ok, reason, cause)
		}
		if _, allowed := permission.NewStore(root).Match("shell", req); allowed {
			t.Fatal("once capability persisted as a project grant")
		}
	}
}

func TestCapabilityApprovalCannotCrossProjectSwitch(t *testing.T) {
	e := newPermTestExecutor(t)
	a, b := t.TempDir(), t.TempDir()
	e.SetProjectStore(a)
	req := core.PermissionRequest{Capabilities: []string{core.CapNetOutbound}}
	tool := &permissionTool{}
	_, ok, _, _ := e.tryPermissionEscalation(t.Context(), tool.ExecuteWithPermission, core.ToolCallItem{Name: "shell"}, testPermissionError{req: req}, func(protocol.ConfirmRequest) protocol.ConfirmReply {
		e.SetProjectStore(b)
		return protocol.AllowRemembered()
	}, escalationApproval{}, false, false)
	if ok || tool.privilegedCalls != 0 {
		t.Fatal("capability approval survived a project switch")
	}
	if _, allowed := permission.NewStore(b).Match("shell", req); allowed {
		t.Fatal("capability grant leaked into another project")
	}
}

func TestCombinedApprovalPreservesOnceScope(t *testing.T) {
	e := newPermTestExecutor(t)
	call := core.ToolCallItem{Name: "shell", Args: map[string]any{"command": `echo "unterminated`}}
	d := e.evaluatePermission(t.Context(), call, nil)
	predicted := &core.PermissionRequest{Scope: "project", Capabilities: []string{core.CapNetOutbound}}
	reply, ok := e.promptConfirm(call, func(req protocol.ConfirmRequest) protocol.ConfirmReply {
		if req.Approval.CanRemember() {
			t.Error("combined approval broadened once scope")
		}
		return protocol.AllowRemembered()
	}, predicted, d)
	if !ok || reply.Remember {
		t.Fatalf("once approval accepted permanent reply: %+v, %t", reply, ok)
	}
}

func TestApprovalCannotCrossProjectSwitch(t *testing.T) {
	e := newPermTestExecutor(t)
	a, b := t.TempDir(), t.TempDir()
	e.SetProjectStore(a)
	e.SetPermissionPolicy(permission.PermissionsDecl{}, a, a)
	e.workspace.Configure(a, nil)
	e.SetConfirmFn(func(req protocol.ConfirmRequest) protocol.ConfirmReply {
		if req.Approval.RememberWorkspace != a {
			t.Fatalf("unexpected preview project: %+v", req.Approval)
		}
		e.SetProjectStore(b)
		return protocol.AllowRemembered()
	})
	r := e.ExecuteBatch(t.Context(), []core.ToolCallItem{{ID: "switch", Name: "shell", Args: map[string]any{"command": "curl https://a.test"}}})[0]
	if r.Error == "" || len(r.Evidence.Executions) != 0 {
		t.Fatalf("approval survived project switch: %+v", r)
	}
	if rules := permission.NewStore(b).RememberedRules(b); len(rules) != 0 {
		t.Fatalf("project A approval leaked into B: %+v", rules)
	}
}

func TestApprovalPersistsTheOriginalPreviewRules(t *testing.T) {
	e := newPermTestExecutor(t)
	root := t.TempDir()
	e.SetProjectStore(root)
	e.SetPermissionPolicy(permission.PermissionsDecl{}, root, root)
	e.workspace.Configure(root, nil)
	e.SetConfirmFn(func(req protocol.ConfirmRequest) protocol.ConfirmReply {
		if len(req.Approval.RememberRules) != 1 || req.Approval.RememberRules[0].Specifier != "curl *" {
			t.Fatalf("unexpected preview: %+v", req.Approval)
		}
		// A UI/observer's detached DTO is not authority to change the grant.
		req.Approval.RememberRules[0].Specifier = "rm *"
		return protocol.AllowRemembered()
	})
	r := e.ExecuteBatch(t.Context(), []core.ToolCallItem{{ID: "snapshot", Name: "shell", Args: map[string]any{"command": "curl https://a.test"}}})[0]
	if r.Error != "" {
		t.Fatal(r.Error)
	}
	rules := permission.NewStore(root).RememberedRules(root)
	if len(rules) != 1 || rules[0].Specifier != "curl *" {
		t.Fatalf("persisted rules differ from original preview: %+v", rules)
	}
}

func TestExplicitAskDoesNotOfferIneffectivePermanentApproval(t *testing.T) {
	e := newPermTestExecutor(t)
	e.SetPermissionPolicy(permission.PermissionsDecl{Ask: []string{"shell(curl *)"}}, "/repo", "/home/user")
	call := core.ToolCallItem{Name: "shell", Args: map[string]any{"command": "curl https://a.test"}}
	d := e.evaluatePermission(t.Context(), call, nil)
	_, ok := e.promptConfirm(call, func(req protocol.ConfirmRequest) protocol.ConfirmReply {
		if req.Approval.CanRemember() {
			t.Error("mandatory ask offers ineffective permanent approval")
		}
		return protocol.AllowOnce()
	}, nil, d)
	if !ok {
		t.Fatal("one-time approval should remain usable")
	}
}

func TestPermanentApprovalSaveFailureIsReported(t *testing.T) {
	e := newPermTestExecutor(t)
	root := t.TempDir()
	e.SetProjectStore(root)
	e.SetPermissionPolicy(permission.PermissionsDecl{}, root, root)
	e.workspace.Configure(root, nil)
	dir := filepath.Dir(e.permStore.Path())
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "permissions.json")
	if err := os.WriteFile(path, []byte("broken JSON"), 0600); err != nil {
		t.Fatal(err)
	}
	e.SetConfirmFn(func(protocol.ConfirmRequest) protocol.ConfirmReply { return protocol.AllowRemembered() })
	r := e.ExecuteBatch(t.Context(), []core.ToolCallItem{{ID: "save-failure", Name: "shell", Args: map[string]any{"command": "curl https://a.test && python report.py"}}})[0]
	if !strings.Contains(r.Error, "cannot save permanent approval") || strings.Contains(r.Output, "ran") || len(r.Evidence.Executions) != 0 {
		t.Fatalf("permanent save failure was hidden: %+v", r)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "broken JSON" {
		t.Fatalf("failed save replaced existing file: %q, %v", data, err)
	}
	e.SetConfirmFn(func(protocol.ConfirmRequest) protocol.ConfirmReply { return protocol.AllowOnce() })
	r = e.ExecuteBatch(t.Context(), []core.ToolCallItem{{ID: "allow-once", Name: "shell", Args: map[string]any{"command": "curl https://a.test && python report.py"}}})[0]
	if r.Error != "" || r.Output != "ran" {
		t.Fatalf("allow once should remain usable without saving rules: %+v", r)
	}
}

func TestCapabilityReadFailureStopsPrivilegedRetry(t *testing.T) {
	for _, preapproved := range []bool{false, true} {
		t.Run(map[bool]string{false: "separate", true: "combined"}[preapproved], func(t *testing.T) {
			e := newPermTestExecutor(t)
			root := t.TempDir()
			e.SetProjectStore(root)
			path := e.permStore.Path()
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("broken JSON"), 0600); err != nil {
				t.Fatal(err)
			}
			req := core.PermissionRequest{Capabilities: []string{core.CapNetOutbound}, Details: map[string]any{"workspace": root}}
			call := core.ToolCallItem{ID: "capability-read", Name: "shell"}
			tool := &permissionTool{}
			confirmed := false
			confirm := func(protocol.ConfirmRequest) protocol.ConfirmReply {
				confirmed = true
				return protocol.AllowRemembered()
			}
			_, ok, reason, cause := e.tryPermissionEscalation(t.Context(), tool.ExecuteWithPermission, call, testPermissionError{req: req}, confirm, escalationApproval{remember: true, predicted: &req}, preapproved, false)
			if ok || tool.privilegedCalls != 0 || confirmed || reason != failPermissionStore || cause == nil {
				t.Fatalf("capability read failure was hidden: ok=%t calls=%d confirmed=%t reason=%v cause=%v", ok, tool.privilegedCalls, confirmed, reason, cause)
			}
			if message := permissionFailureMessage(call, testPermissionError{req: req}, reason, cause); !strings.Contains(message, "cannot read capability permission rules") {
				t.Fatalf("missing capability read error: %s", message)
			}
			// Recovery requires readable policy even when the next approval is once-only.
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			once := func(protocol.ConfirmRequest) protocol.ConfirmReply { return protocol.AllowOnce() }
			_, ok, reason, cause = e.tryPermissionEscalation(t.Context(), tool.ExecuteWithPermission, call, testPermissionError{req: req}, once, escalationApproval{predicted: &req}, preapproved, false)
			if !ok || tool.privilegedCalls != 1 {
				t.Fatalf("readable policy did not recover: ok=%t calls=%d reason=%v cause=%v", ok, tool.privilegedCalls, reason, cause)
			}
		})
	}
}

func TestPermanentCapabilitySaveFailureStopsPrivilegedRetry(t *testing.T) {
	e := newPermTestExecutor(t)
	root := t.TempDir()
	e.SetProjectStore(root)
	req := core.PermissionRequest{Capabilities: []string{core.CapNetOutbound}, Details: map[string]any{"workspace": root}}
	call := core.ToolCallItem{ID: "capability-save", Name: "shell"}
	tool := &permissionTool{}
	confirmed := false
	confirm := func(protocol.ConfirmRequest) protocol.ConfirmReply {
		confirmed = true
		path := e.permStore.Path()
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("broken after approval opened"), 0600); err != nil {
			t.Fatal(err)
		}
		return protocol.AllowRemembered()
	}
	_, ok, reason, cause := e.tryPermissionEscalation(t.Context(), tool.ExecuteWithPermission, call, testPermissionError{req: req}, confirm, escalationApproval{}, false, false)
	if ok || tool.privilegedCalls != 0 || !confirmed || reason != failSaveApproval || cause == nil {
		t.Fatalf("post-approval save failure was hidden: ok=%t calls=%d confirmed=%t reason=%v cause=%v", ok, tool.privilegedCalls, confirmed, reason, cause)
	}
	if message := permissionFailureMessage(call, testPermissionError{req: req}, reason, cause); !strings.Contains(message, "cannot save permanent capability approval") {
		t.Fatalf("missing capability save error: %s", message)
	}
}

func TestShellRememberReusesCommandsAcrossScripts(t *testing.T) {
	e := newPermTestExecutor(t)
	root := t.TempDir()
	e.SetProjectStore(root)
	e.SetPermissionPolicy(permission.PermissionsDecl{}, root, root)
	first := core.ToolCallItem{Name: "shell", Args: map[string]any{"command": `curl https://first.test && printf '%s' "$(date)"`}}
	e.rememberAllowRule("shell", first.Args)
	// Reusable prefixes are remembered; the exact statement holding "$(date)"
	// is reused only within this session and never persisted.
	if d := e.evaluatePermission(t.Context(), core.ToolCallItem{Name: "shell", Args: map[string]any{"command": `curl https://second.test`}}, nil); d.prompt || d.block {
		t.Fatalf("reusable prefix not reused: %+v", d)
	}
	if d := e.evaluatePermission(t.Context(), core.ToolCallItem{Name: "shell", Args: map[string]any{"command": `curl https://second.test && printf '%s' "$(date)"`}}, nil); d.prompt || d.block {
		t.Fatalf("exact dynamic statement should be reused within its session: %+v", d)
	}
	second := core.ToolCallItem{Name: "shell", Args: map[string]any{"command": `curl https://second.test && python report.py`}}
	d := e.evaluatePermission(t.Context(), second, nil)
	if !d.prompt {
		t.Fatal("new python command must require approval")
	}
	var preview *protocol.ApprovalContext
	_, ok := e.promptConfirm(second, func(req protocol.ConfirmRequest) protocol.ConfirmReply {
		preview = req.Approval.Clone()
		return protocol.AllowRemembered()
	}, nil, d)
	if !ok || preview == nil || len(preview.RememberRules) != 1 || preview.RememberRules[0].Specifier != "python *" {
		t.Fatalf("preview repeated approved curl: %+v", preview)
	}
	if len(preview.ApprovedRules) != 1 || preview.ApprovedRules[0].Specifier != "curl *" {
		t.Fatalf("missing reused curl grant: %+v", preview)
	}
	e.rememberAllowRule("shell", second.Args)
	third := core.ToolCallItem{Name: "shell", Args: map[string]any{"command": `curl https://third.test && python another.py`}}
	if d := e.evaluatePermission(t.Context(), third, nil); d.prompt || d.block {
		t.Fatalf("combined permanent command grants not reused: %+v", d)
	}
	restarted := newPermTestExecutor(t)
	restarted.SetProjectStore(root)
	restarted.SetPermissionPolicy(permission.PermissionsDecl{}, root, root)
	if d := restarted.evaluatePermission(t.Context(), third, nil); d.prompt || d.block {
		t.Fatalf("grants were not persisted across reload: %+v", d)
	}
}

func TestApprovalPreviewMatchesRememberedRules(t *testing.T) {
	for _, command := range []string{`echo hello && date`, `bash -c 'echo ok'`} {
		t.Run(command, func(t *testing.T) {
			e := newPermTestExecutor(t)
			root := t.TempDir()
			e.SetProjectStore(root)
			e.SetPermissionPolicy(permission.PermissionsDecl{}, root, root)
			call := core.ToolCallItem{Name: "shell", Args: map[string]any{"command": command}}
			decision := e.evaluatePermission(t.Context(), call, nil)
			var approval *protocol.ApprovalContext
			_, ok := e.promptConfirm(call, func(req protocol.ConfirmRequest) protocol.ConfirmReply {
				approval = req.Approval.Clone()
				return protocol.AllowRemembered()
			}, nil, decision)
			if !ok || approval == nil || approval.RememberWorkspace != root || len(approval.RememberRules) == 0 {
				t.Fatalf("missing preview: %+v", approval)
			}
			e.rememberAllowRule("shell", call.Args)
			// Exact statements are session-scoped, so only the persistent part
			// of the preview may reach the file.
			var persistent []protocol.ApprovalRememberRule
			for _, rule := range approval.RememberRules {
				if !rule.Session() {
					persistent = append(persistent, rule)
				}
			}
			rules := e.permStore.RememberedRules(root)
			if len(rules) != len(persistent) {
				t.Fatalf("persisted=%+v preview=%+v", rules, approval.RememberRules)
			}
			for i, rule := range rules {
				preview := persistent[i]
				if preview.Tool != rule.Tool || preview.Literal != rule.Literal || rule.Literal == "" && preview.Specifier != rule.Specifier {
					t.Fatalf("preview differs from saved rule: %+v != %+v", preview, rule)
				}
			}
		})
	}
}

func TestApprovalExplainsExplicitAskSource(t *testing.T) {
	e := newPermTestExecutor(t)
	e.SetPermissionPolicy(permission.PermissionsDecl{Ask: []string{"shell(make *)"}}, "/repo", "/home/user")
	d := e.evaluatePermission(t.Context(), core.ToolCallItem{Name: "shell", Args: map[string]any{"command": "make test"}}, nil)
	if !d.prompt || d.approval == nil || d.approval.Source != "user_rule" || d.approval.Rule != "shell(make *)" {
		t.Fatalf("missing rule source: %+v", d.approval)
	}
}

// Session grants stay in the executor's memory, so they must not follow the
// user into a different project: that project has its own permissions file.
func TestSessionGrantDoesNotLeakIntoAnotherProject(t *testing.T) {
	e := newPermTestExecutor(t)
	first, second := t.TempDir(), t.TempDir()
	e.SetProjectStore(first)
	e.SetPermissionPolicy(permission.PermissionsDecl{}, first, first)
	call := core.ToolCallItem{Name: "shell", Args: map[string]any{"command": `curl https://a.test > out.json`}}
	if d := e.evaluatePermission(t.Context(), call, nil); !d.prompt {
		t.Fatalf("expected a prompt before any grant: %+v", d)
	}
	if err := e.rememberAllowRule("shell", call.Args); err != nil {
		t.Fatalf("remember: %v", err)
	}
	if d := e.evaluatePermission(t.Context(), call, nil); d.prompt || d.block {
		t.Fatalf("session grant not reused in its own project: %+v", d)
	}
	e.SetProjectStore(second)
	e.SetPermissionPolicy(permission.PermissionsDecl{}, second, second)
	if d := e.evaluatePermission(t.Context(), call, nil); !d.prompt {
		t.Fatalf("session grant leaked into another project: %+v", d)
	}
}

// Repeatedly approving the same dynamic command must not accumulate copies of
// its session rule in the engine.
func TestRepeatedSessionApprovalDoesNotDuplicateRules(t *testing.T) {
	e := newPermTestExecutor(t)
	root := t.TempDir()
	e.SetProjectStore(root)
	e.SetPermissionPolicy(permission.PermissionsDecl{}, root, root)
	call := core.ToolCallItem{Name: "shell", Args: map[string]any{"command": `curl https://a.test > out.json`}}
	for i := 0; i < 3; i++ {
		if err := e.rememberAllowRule("shell", call.Args); err != nil {
			t.Fatalf("remember %d: %v", i, err)
		}
	}
	e.fnMu.RLock()
	count := len(e.permSession)
	e.fnMu.RUnlock()
	if count != 1 {
		t.Fatalf("session rules duplicated: %d entries", count)
	}
}

// rememberAllowRule persists an allow rule when the user approves an "ask"
// prompt with "remember". It derives the rule specifier from the tool call so
// future matching calls auto-approve, then rebuilds the engine immediately.
func (e *Executor) rememberAllowRule(toolName string, args map[string]any) error {
	policy := e.permissionSnapshot()
	if policy.store == nil || policy.workspace == "" || policy.engine == nil {
		return nil
	}
	rules := pendingRulesToRemember(policy.engine, toolName, args, policy.workspace, policy.home)
	return e.rememberApprovedRules(policy, rules)
}
