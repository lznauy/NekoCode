package runner

import (
	"context"
	"testing"

	tools "nekocode/bot/extension/tool"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/permission"
	"nekocode/protocol"
)

// escFixture wires a minimal executor for direct escalation tests and counts
// confirm-dialog invocations.
func escFixture(t *testing.T) (*Executor, *int) {
	t.Helper()
	e := newTestExecutor(fakeRegistry{})
	e.workspace.Configure("/repo", nil)
	e.SetPermissionPolicy(permission.PermissionsDecl{}, "/repo", "/home/user")
	confirmCalls := 0
	e.SetConfirmFn(func(protocol.ConfirmRequest) protocol.ConfirmReply {
		confirmCalls++
		return protocol.Deny()
	})
	return e, &confirmCalls
}

// permErrFor builds the error a privileged tool returns when the sandbox
// cannot satisfy it.
func permErrFor() error {
	return core.RequiredPermissionError{Request: core.PermissionRequest{
		Reason:       "test needs write outside workspace",
		Capabilities: []string{core.CapFsWritePath},
	}}
}

// privilegedOK is a PrivilegedFunc that succeeds when invoked.
func privilegedOK(context.Context, map[string]any, core.PermissionRequest) (core.ToolOutput, error) {
	return core.ToolOutput{Text: "escalated-ok"}, nil
}

func TestJevSafeEscalationSkipsDialog(t *testing.T) {
	e, confirmCalls := escFixture(t)
	tc := core.ToolCallItem{ID: "1", Name: "shell", Args: map[string]any{"command": "echo hi"}}

	out, ok, _, cause := e.tryPermissionEscalation(
		context.Background(), privilegedOK, tc, permErrFor(),
		e.ConfirmFn(), escalationApproval{}, false, true)
	if !ok {
		t.Fatalf("jev-safe escalation should succeed, cause=%v", cause)
	}
	if out.Text != "escalated-ok" {
		t.Fatalf("out = %q", out.Text)
	}
	if *confirmCalls != 0 {
		t.Fatalf("confirm dialog must not fire for a jev-safe grant, got %d calls", *confirmCalls)
	}
}

func TestJevVerdictMissingStillPrompts(t *testing.T) {
	e, confirmCalls := escFixture(t)
	tc := core.ToolCallItem{ID: "1", Name: "shell", Args: map[string]any{"command": "echo hi"}}

	_, ok, reason, _ := e.tryPermissionEscalation(
		context.Background(), privilegedOK, tc, permErrFor(),
		e.ConfirmFn(), escalationApproval{}, false, false)
	if ok {
		t.Fatal("without a jev verdict the escalation must go through the dialog")
	}
	if reason != failUserDenied {
		t.Fatalf("reason = %v, want failUserDenied (fixture denies)", reason)
	}
	if *confirmCalls != 1 {
		t.Fatalf("confirm dialog should fire exactly once, got %d", *confirmCalls)
	}
}

func TestJevSafeGrantsHostExecutionWithoutDialog(t *testing.T) {
	e, confirmCalls := escFixture(t)
	// Host execution (full sandbox removal) follows the same jev-safe fast
	// path: the owner opted into auto mode, and the judge's verdict is about
	// the command itself, not the execution environment. Deny rules and
	// ask-rule hits still keep the dialog mandatory.
	hostErr := core.RequiredPermissionError{Request: core.PermissionRequest{
		Reason:       "test needs host execution",
		Capabilities: []string{core.CapProcessHost},
	}}
	tc := core.ToolCallItem{ID: "1", Name: "shell", Args: map[string]any{"command": "echo hi"}}

	out, ok, _, cause := e.tryPermissionEscalation(
		context.Background(), privilegedOK, tc, hostErr,
		e.ConfirmFn(), escalationApproval{}, false, true)
	if !ok {
		t.Fatalf("jev-safe host execution should be granted, cause=%v", cause)
	}
	if out.Text != "escalated-ok" {
		t.Fatalf("out = %q", out.Text)
	}
	if *confirmCalls != 0 {
		t.Fatalf("dialog must not fire for a jev-safe host grant, got %d calls", *confirmCalls)
	}
}

// The fixture must keep the tools import even as assertions evolve.
var _ tools.PrivilegedFunc = privilegedOK
