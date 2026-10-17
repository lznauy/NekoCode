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
func privilegedOK(context.Context, map[string]any, core.PermissionRequest) (string, error) {
	return "escalated-ok", nil
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
	if out != "escalated-ok" {
		t.Fatalf("out = %q", out)
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

func TestJevSafeDoesNotBypassHostExecutionPrompt(t *testing.T) {
	e, confirmCalls := escFixture(t)
	// A request for CapProcessHost removes the sandbox entirely — even a
	// jev-safe verdict must not grant it silently; the dialog always fires
	// (mirroring the never-persist rule for remembered grants).
	hostErr := core.RequiredPermissionError{Request: core.PermissionRequest{
		Reason:       "test needs host execution",
		Capabilities: []string{core.CapProcessHost},
	}}
	tc := core.ToolCallItem{ID: "1", Name: "shell", Args: map[string]any{"command": "echo hi"}}

	_, ok, reason, _ := e.tryPermissionEscalation(
		context.Background(), privilegedOK, tc, hostErr,
		e.ConfirmFn(), escalationApproval{}, false, true)
	if ok {
		t.Fatal("host execution must always require the explicit dialog")
	}
	if reason != failUserDenied {
		t.Fatalf("reason = %v, want failUserDenied (fixture denies)", reason)
	}
	if *confirmCalls != 1 {
		t.Fatalf("dialog should fire for host execution, got %d calls", *confirmCalls)
	}
}

// The fixture must keep the tools import even as assertions evolve.
var _ tools.PrivilegedFunc = privilegedOK
