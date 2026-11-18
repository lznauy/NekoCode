package core

import (
	"context"
	"testing"

	"nekocode/bot/command"
	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/protocol"
)

// ExecuteLocalCommand: during-task-safe commands run without a run; context
// commands defer to the run path; plain text is not a command.
func TestExecuteLocalCommandFork(t *testing.T) {
	var full bool
	b := &Bot{ctxMgr: ctxmgr.New(ctxmgr.Config{})}
	b.cmd = command.New(command.Deps{
		CtxMgr:        b.ctxMgr,
		SetFullAccess: func(on bool) { full = on },
		GetFullAccess: func() bool { return full },
	})
	ctx := protocol.WithCommandStatusDisplay(context.Background())

	out, status := b.ExecuteLocalCommand(ctx, "/permission full")
	if status != protocol.LocalCommandExecuted || !full || out != "" {
		t.Fatalf("local command: status=%v full=%v out=%q", status, full, out)
	}

	if _, status = b.ExecuteLocalCommand(ctx, "/new"); status != protocol.LocalCommandRequiresIdle {
		t.Fatalf("/new: status=%v, want RequiresIdle", status)
	}
	if _, status = b.ExecuteLocalCommand(ctx, "hello world"); status != protocol.LocalCommandNotCommand {
		t.Fatalf("plain text: status=%v, want NotCommand", status)
	}
}
