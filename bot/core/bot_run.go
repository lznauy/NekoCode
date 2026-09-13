package core

import (
	"context"
	"errors"

	agentcore "nekocode/bot/agent"
	"nekocode/bot/config"
	"nekocode/bot/extension/plugin"
	"nekocode/bot/extension/tool/runtime/permission"
	"nekocode/protocol"
)

func (b *Bot) Steer(ctx context.Context, msg string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return b.getAgent().TrySteer(msg)
}

func (b *Bot) getAgent() *agentcore.Agent {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.ag
}

func (b *Bot) Run(ctx context.Context, input string, host RunHost) (string, error) {
	b.ensureSessionIdentity()
	release := b.bindHost(host)
	defer release()

	ag := b.getAgent()
	finished := make(chan struct{})
	watchDone := make(chan struct{})
	go func() {
		defer close(watchDone)
		select {
		case <-ctx.Done():
			ag.Abort()
		case <-finished:
		}
	}()
	defer func() {
		close(finished)
		<-watchDone
	}()
	output, err := b.runAgent(ctx, input, host.Step)
	return output, err
}

func (b *Bot) runAgent(ctx context.Context, input string, onStep func(ev protocol.StepEvent)) (string, error) {
	sessionID := b.sess.CurrentID()
	ag := b.getAgent()
	defer func() {
		ag.Executor().SetPlanMode(false)
		b.ctxMgr.SetRuntimePolicy("")
	}()
	if b.checkpoints != nil {
		if _, err := b.checkpoints.BeginMessage(sessionID, input); err != nil {
			return "", err
		}
	}
	result := ag.Run(input, onStep)
	var checkpointErr error
	if b.checkpoints != nil {
		checkpointErr = b.checkpoints.Finish(sessionID)
	}
	// The trailing compaction is housekeeping, not part of the run's outcome:
	// after a caller-side cancellation it reports context.Canceled, which
	// would surface as a run failure even though the turn itself was fine.
	_, compactionErr := b.ctxMgr.AutoCompactIfNeeded(context.Background())
	if compactionErr != nil && (errors.Is(compactionErr, context.Canceled) || errors.Is(compactionErr, context.DeadlineExceeded)) {
		compactionErr = nil
	}
	result.Error = errors.Join(
		result.Error,
		checkpointErr,
		compactionErr,
		b.saveSession(),
	)
	return result.FinalOutput, result.Error
}

// bindHost exposes the current synchronous operation to callbacks registered
// on long-lived agents and tools.
func (b *Bot) bindHost(host RunHost) func() {
	b.hostMu.Lock()
	b.runHost = host
	b.hostMu.Unlock()
	return func() {
		b.hostMu.Lock()
		b.runHost = nil
		b.hostMu.Unlock()
	}
}

func (b *Bot) currentHost() RunHost {
	b.hostMu.RLock()
	defer b.hostMu.RUnlock()
	return b.runHost
}

func (b *Bot) stream(delta string) {
	if host := b.currentHost(); host != nil {
		host.Text(delta)
	}
}

func (b *Bot) reasoning(delta string) {
	if host := b.currentHost(); host != nil {
		host.Reason(delta)
	}
}

func (b *Bot) phase(phase string) {
	if host := b.currentHost(); host != nil {
		host.Phase(phase)
	}
}

func (b *Bot) todos(items []protocol.TodoItem) {
	if host := b.currentHost(); host != nil {
		host.Todos(items)
	}
}

func (b *Bot) confirm(request protocol.ConfirmRequest) protocol.ConfirmReply {
	if host := b.currentHost(); host != nil {
		return host.Confirm(request)
	}
	return protocol.ConfirmReply{Allowed: false}
}

func (b *Bot) ask(request protocol.QuestionRequest) protocol.QuestionReply {
	if host := b.currentHost(); host != nil {
		return host.Ask(request)
	}
	return protocol.QuestionReply{Rejected: true}
}

func (b *Bot) configureAgent(ag *agentcore.Agent) {
	if ag == nil {
		return
	}
	ag.Executor().SetProjectStore(b.cwd)
	ag.Executor().SetPermissionPolicy(toPermDecl(b.cfg.Permissions), b.cwd, b.home)
}

func (b *Bot) confirmInstall(source string, candidate *plugin.Plugin, remote bool) bool {
	reply := b.confirm(protocol.NewConfirmRequest(
		"/plugin install",
		map[string]any{"source": source, "summary": plugin.ConfirmSummary(candidate, remote)},
		protocol.ConfirmKindInstall,
	))
	return reply.Allowed
}

func toPermDecl(config *config.PermissionsConfig) permission.PermissionsDecl {
	if config == nil {
		return permission.PermissionsDecl{}
	}
	return permission.PermissionsDecl{
		Allow: config.Allow, Ask: config.Ask, Deny: config.Deny,
		Sandbox: toSandboxDecl(config.Sandbox),
	}
}

func toSandboxDecl(input map[string]config.SandboxConfig) map[string]permission.SandboxProfile {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]permission.SandboxProfile, len(input))
	for rule, profile := range input {
		output[rule] = permission.SandboxProfile{
			SandboxMode: profile.SandboxMode, Network: profile.Network,
			WritableRoots: append([]string(nil), profile.WritableRoots...),
		}
	}
	return output
}

func (b *Bot) CommandMenu(ctx context.Context, input string) (protocol.CommandMenu, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.cmd == nil {
		return protocol.CommandMenu{}, false
	}
	return b.cmd.Menu(ctx, input)
}

func (b *Bot) ExecuteCommand(ctx context.Context, input string, host RunHost) (protocol.CommandResult, error) {
	release := b.bindHost(host)
	defer release()

	resp, handled := b.cmd.Execute(ctx, input, b.ctxMgr)
	if !handled {
		return protocol.CommandResult{Action: protocol.CommandIgnored}, nil
	}

	if err := b.saveSession(); err != nil {
		return protocol.CommandResult{}, err
	}

	wantsAgent := b.cmd.TakeContinuation()
	result := protocol.CommandResult{
		Action: protocol.CommandHandled,
		Output: resp,
	}
	if wantsAgent {
		result.Action = protocol.CommandContinue
	}
	return result, nil
}

// ExecuteLocalCommand runs a during-task-safe command without a run
// lifecycle: no run ID, no busy check, no session save (local commands do
// not touch conversation context by definition). Commands that need the run
// path report LocalCommandRequiresIdle so callers can route or reject them.
func (b *Bot) ExecuteLocalCommand(ctx context.Context, input string) (string, protocol.LocalCommandResult) {
	isCommand, duringTask := b.cmd.CommandAvailability(input)
	if !isCommand {
		return "", protocol.LocalCommandNotCommand
	}
	if !duringTask {
		return "", protocol.LocalCommandRequiresIdle
	}
	out, handled := b.cmd.Execute(ctx, input, b.ctxMgr)
	if !handled {
		return "", protocol.LocalCommandNotCommand
	}
	return out, protocol.LocalCommandExecuted
}
