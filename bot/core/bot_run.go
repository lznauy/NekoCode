package core

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/google/uuid"
	agentcore "nekocode/bot/agent"
	"nekocode/bot/config"
	"nekocode/bot/extension/plugin"
	"nekocode/bot/extension/tool/runtime/permission"
	"nekocode/bot/provider/types"
	"nekocode/mode"
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

func (b *Bot) Run(ctx context.Context, input string, images []types.MessageImage, host RunHost) (string, error) {
	if !b.operationMu.TryLock() {
		return "", errors.New("bot: another operation is running")
	}
	defer b.operationMu.Unlock()
	b.flushPendingProjectReload()
	b.ensureSessionIdentity()
	if submitter, ok := host.(protocol.BackgroundSubmitter); ok {
		ctx = protocol.WithBackgroundSubmitter(ctx, submitter)
	}
	release := b.bindHost(host)
	defer release()

	output, err := b.runAgent(ctx, input, images, host.Step)
	return output, err
}

func (b *Bot) runAgent(ctx context.Context, input string, images []types.MessageImage, onStep func(ev protocol.StepEvent)) (string, error) {
	if protocol.ExecutionFromContext(ctx).RunID == "" {
		ctx = protocol.WithExecutionIdentity(ctx, protocol.ExecutionIdentity{RunID: uuid.NewString()})
	}
	snapshot, captured := mode.FromContext(ctx)
	if !captured {
		var err error
		snapshot, err = b.ResolveInteractionMode(input)
		if err != nil {
			return "", err
		}
	}
	definition, err := mode.Validate(snapshot)
	if err != nil {
		return "", err
	}
	// Direct and delegated strategies share the agent loop. The captured policy
	// selects direct execution or coordination-only dispatch into task workers.
	run, err := interactionStrategy(definition.Strategy)
	if err != nil {
		return "", err
	}
	ctx = mode.WithSnapshot(ctx, snapshot)
	sessionID := b.sess.CurrentID()
	ag := b.getAgent()
	if err := ag.Executor().SetInteractionMode(snapshot); err != nil {
		return "", err
	}
	b.ctxMgr.SetRuntimePolicy(definition.Prompt)
	defer func() {
		ag.Executor().SetPlanMode(false)
		b.ctxMgr.SetRuntimePolicy("")
	}()
	if b.checkpoints != nil {
		if _, err := b.checkpoints.BeginMessage(sessionID, input); err != nil {
			return "", err
		}
	}
	result := run(ag, ctx, input, images, onStep)
	var checkpointErr error
	if b.checkpoints != nil {
		checkpointErr = b.checkpoints.Finish(sessionID)
	}
	// The trailing compaction is housekeeping, not part of the run's outcome:
	// after a caller-side cancellation it reports context.Canceled, which
	// would surface as a run failure even though the turn itself was fine.
	compactionCtx := protocol.WithPolicyConfiguration(context.WithoutCancel(ctx), b.policy.Configuration())
	compactionCtx = protocol.WithProfileConfiguration(compactionCtx, "")
	compactionCtx = protocol.WithAccessObservation(compactionCtx, ag.Executor().AccessObservation())
	_, compactionErr := b.ctxMgr.AutoCompactIfNeeded(compactionCtx)
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

// resetStream forwards a discard signal to hosts that can retract streamed
// text. Hosts without the extension simply keep the text, which is the old
// behavior.
func (b *Bot) resetStream() {
	if host := b.currentHost(); host != nil {
		if resetter, ok := host.(RunTextResetter); ok {
			resetter.ResetText()
		}
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
			WritableRoots:   append([]string(nil), profile.WritableRoots...),
			OutboundDomains: slices.Clone(profile.OutboundDomains),
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
	if b.reloadView != nil {
		input = strings.TrimSpace(input)
		if !strings.HasPrefix(input, "/") && !strings.HasPrefix(input, "$") {
			return protocol.CommandMenu{}, false
		}
		return protocol.CommandMenu{Title: "Workspace refreshing", Empty: "Command choices will update when refresh completes."}, true
	}
	return b.cmd.Menu(ctx, input)
}

func (b *Bot) ExecuteCommand(ctx context.Context, input string, host RunHost) (protocol.CommandResult, error) {
	if !b.operationMu.TryLock() {
		return protocol.CommandResult{}, errors.New("bot: another operation is running")
	}
	defer b.operationMu.Unlock()
	release := b.bindHost(host)
	defer release()

	previousMode := b.InteractionMode()
	previousSessionID := b.sess.CurrentID()
	resp, handled := b.cmd.Execute(ctx, input, b.ctxMgr)
	if !handled {
		return protocol.CommandResult{Action: protocol.CommandIgnored}, nil
	}

	// A command may already have committed a session switch. Its target's
	// policy must not be replaced with the source policy if persistence fails.
	sameSession := b.sess.CurrentID() == previousSessionID
	if err := b.saveSession(); err != nil {
		if sameSession {
			b.storeInteractionMode(string(previousMode))
		}
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
	// Run directly on the parser: Handler.Execute also resets command
	// continuation state, which a during-task status command must not touch.
	parser := b.cmd.Parser()
	out, handled := parser.Execute(ctx, parser.Parse(input))
	if !handled {
		return "", protocol.LocalCommandNotCommand
	}
	return out, protocol.LocalCommandExecuted
}
