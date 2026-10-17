package runner

import (
	"context"
	"testing"

	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/permission"
	"nekocode/protocol"
)

// TestJevSafeVerdictFlowsThroughShellPath guards the real chain this time:
// a shell command that the auto-mode judge explicitly passes must reach the
// escalation stage with jevSafe set. The first implementation of the verdict
// only annotated the generic (non-shell) evaluation path, while every shell
// command takes the evaluateAs shortcut — so the flag was silently dropped
// and the capability dialog still popped.
func TestJevSafeVerdictFlowsThroughShellPath(t *testing.T) {
	e := newPermTestExecutor(t)
	e.SetConfirmFn(func(protocol.ConfirmRequest) protocol.ConfirmReply { return protocol.Deny() })
	// Auto mode with a judge that confidently passes the command.
	e.SetPermissionMode(permission.ModeAuto)
	e.SetBashAutoJudge(func(_ context.Context, _ string) (dangerous bool, ok bool) {
		return false, true
	})

	tc := core.ToolCallItem{ID: "1", Name: "shell", Args: map[string]any{"command": "curl -sS https://example.com/api"}}
	dec := e.evaluatePermission(context.Background(), tc, nil)

	if dec.block {
		t.Fatalf("judge-passed command was blocked: %s", dec.reason)
	}
	if dec.prompt {
		t.Fatal("judge-passed command should not prompt")
	}
	if !dec.jevSafe {
		t.Fatalf("jevSafe not set on the shell evaluation path — the escalation dialog would pop again (reason=%q)", dec.reason)
	}
}

// Without a judge verdict the flag must stay off — the dialog is legitimate.
func TestJevSafeVerdictOffWithoutJudge(t *testing.T) {
	e := newPermTestExecutor(t)
	e.SetPermissionMode(permission.ModeManual)
	e.SetConfirmFn(func(protocol.ConfirmRequest) protocol.ConfirmReply { return protocol.AllowOnce() })

	tc := core.ToolCallItem{ID: "1", Name: "shell", Args: map[string]any{"command": "echo hi"}}
	dec := e.evaluatePermission(context.Background(), tc, nil)
	if dec.jevSafe {
		t.Fatal("jevSafe must require an explicit safe verdict from the judge")
	}
}
