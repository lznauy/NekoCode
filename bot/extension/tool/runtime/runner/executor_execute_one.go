package runner

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/execution"
	"nekocode/bot/extension/tool/runtime/permission"
	"nekocode/bot/extension/tool/runtime/workspace"
	"nekocode/logger"
	"nekocode/protocol"
)

func (e *Executor) executeOne(ctx context.Context, tc core.ToolCallItem) core.ToolCallResult {
	entry, err := e.registry.Lookup(tc.Name)
	if err != nil {
		return core.ToolCallResult{ID: tc.ID, Name: tc.Name, Error: err.Error()}
	}
	if tc.EffectiveName == "" && entry.ResolveTarget != nil {
		if target, ok := entry.ResolveTarget(tc.Args); ok {
			tc.EffectiveName = target.Name
			tc.EffectiveArgs = target.Args
		}
	}
	resultName, _ := tc.Effective()
	tool := entry.Tool
	tc = e.applySandboxProfile(tc)

	phaseFn, confirmFn, planMode := e.callbacks()
	if phaseFn != nil {
		phaseFn(protocol.PhaseRunning + " " + resultName)
	}
	if planMode && !e.planAllows(tc.Name, entry.PlanAllowed) {
		return core.ToolCallResult{ID: tc.ID, Name: resultName, Error: "plan mode: blocked"}
	}

	// Predict the escalation request a privileged tool will raise from explicit
	// sandbox arguments. This lets the command approval dialog offer a single
	// unified decision without exposing legacy capability args.
	// If a grant already matches, skip the engine's default "ask" prompt.
	predictedReq := e.permissionRequestFromEntry(entry, tc.Args)
	var preApproval *escalationApproval
	if dec := e.evaluatePermission(tc, predictedReq); dec.block {
		return core.ToolCallResult{ID: tc.ID, Name: resultName, Error: dec.reason}
	} else if dec.prompt {
		reply, ok := e.promptConfirm(tc, confirmFn, predictedReq, dec)
		if !ok {
			return core.ToolCallResult{ID: tc.ID, Name: resultName, Error: "cancelled"}
		}
		if reply.Remember {
			toolName := tc.Name
			args := tc.Args
			if dec.rememberTool != "" {
				toolName = dec.rememberTool
				args = dec.rememberArgs
			}
			e.rememberAllowRule(toolName, args, dec.matchedRule)
		}
		if predictedReq != nil {
			preApproval = &escalationApproval{remember: reply.Remember, predicted: predictedReq}
		}
	}

	var errMsg string
	var ok bool
	if tc, errMsg, ok = e.ensureWorkspaceAccess(tc, confirmFn); !ok {
		return core.ToolCallResult{ID: tc.ID, Name: resultName, Error: errMsg}
	}
	captured, captureErr := e.captureMutationPaths(tc)
	if captureErr != nil {
		e.finalizeMutationPaths(captured)
		return core.ToolCallResult{ID: tc.ID, Name: resultName, Error: captureErr.Error()}
	}
	defer e.finalizeMutationPaths(captured)

	paths := toolPaths(tc)
	output, execErr := e.callTool(ctx, tool, tc)
	if execErr != nil {
		preApproved := escalationApproval{}
		if preApproval != nil {
			preApproved = *preApproval
		}
		escOut, escOk, escReason, escCause := e.tryPermissionEscalation(ctx, entry.Privileged, tc, execErr, confirmFn, preApproved, preApproval != nil)
		if escOk {
			e.invalidateMutatedPaths(tc.Name, paths)
			return core.ToolCallResult{ID: tc.ID, Name: resultName, Output: formatOutput(resultName, escOut)}
		}
		return core.ToolCallResult{ID: tc.ID, Name: resultName, Error: permissionFailureMessage(tc, execErr, escReason, escCause)}
	}

	e.invalidateMutatedPaths(tc.Name, paths)
	return core.ToolCallResult{ID: tc.ID, Name: resultName, Output: formatOutput(resultName, output)}
}

func (e *Executor) captureMutationPaths(tc core.ToolCallItem) ([]string, error) {
	if (tc.Name != "write" && tc.Name != "edit") || e.state == nil || e.state.Checkpoints == nil {
		return nil, nil
	}
	paths := toolPaths(tc)
	captured := make([]string, 0, len(paths))
	for _, path := range paths {
		if err := e.state.Checkpoints.Capture(path); err != nil {
			return captured, err
		}
		captured = append(captured, path)
	}
	return captured, nil
}

func (e *Executor) finalizeMutationPaths(paths []string) {
	if e.state == nil || e.state.Checkpoints == nil {
		return
	}
	for _, path := range paths {
		if err := e.state.Checkpoints.Finalize(path); err != nil {
			logger.Log("checkpoint finalize %s: %v", path, err)
		}
	}
}

// permissionDecision is the outcome of evaluating the engine for one call.
type permissionDecision struct {
	block        bool
	reason       string
	prompt       bool
	promptTool   string
	promptArgs   map[string]any
	matchedRule  permission.Rule
	rememberTool string
	rememberArgs map[string]any
	approval     *protocol.ApprovalContext
}

// evaluatePermission runs the rule engine and either blocks, prompts, or
// allows the call. If no explicit rule matched (engine fell to its default
// effect) AND a predicted capability grant already covers the call, we skip
// the basic prompt. Explicit ask rules (rm *, git push *, ...) always win:
// they are command-level safety prompts the user opted into, orthogonal to
// capability grants, so a net.outbound grant MUST NOT silence an "ask rm *".
func (e *Executor) evaluatePermission(tc core.ToolCallItem, predictedReq *core.PermissionRequest) permissionDecision {
	engine, ws, home := e.permissionEngine()
	if cmd, ok := shellCommandForPolicy(tc); ok {
		if decision, hit := e.evaluateAs(engine, ws, home, "shell", map[string]any{"command": cmd}, tc, predictedReq, permission.EffectAsk); hit {
			return decision
		}
	}

	// Delegating tools are evaluated against the effective target attached to
	// their registry entry. The runner does not know concrete proxy protocols.
	if tc.EffectiveName != "" {
		if decision, hit := e.evaluateAs(engine, ws, home, tc.EffectiveName, tc.EffectiveArgs, tc, predictedReq, permission.EffectAsk); hit {
			return decision
		}
	}

	callInfo := permission.BuildCallInfo(tc.Name, tc.Args, ws, home)
	dec := engine.Evaluate(tc.Name, callInfo, defaultPermissionEffect(tc.Name))
	return e.permissionDecisionForRule(dec, tc, predictedReq)
}

// evaluateAs evaluates the engine under an effective tool identity (shell
// with its command, or a capability call's mcp__server__tool target). A
// block/prompt decision is annotated with that identity so the confirm
// dialog and remembered rules name the real tool, and hit is true; an
// allowed call reports hit=false so the caller can fall through.
func (e *Executor) evaluateAs(engine *permission.Engine, ws, home, name string, args map[string]any, tc core.ToolCallItem, predictedReq *core.PermissionRequest, fallback permission.Effect) (permissionDecision, bool) {
	callInfo := permission.BuildCallInfo(name, args, ws, home)
	dec := engine.Evaluate(name, callInfo, fallback)
	decision := e.permissionDecisionForRule(dec, tc, predictedReq)
	if !decision.block && !decision.prompt {
		// A concrete allow for the delegated target is final. Falling through
		// would evaluate the proxy name as well and could re-prompt for
		// "capability" even though mcp__server__tool was explicitly allowed.
		if dec.Effect == permission.EffectAllow && dec.Rule.Tool != "" {
			return permissionDecision{}, true
		}
		return permissionDecision{}, false
	}
	decision.promptTool = name
	decision.promptArgs = args
	decision.rememberTool = name
	decision.rememberArgs = args
	return decision, true
}

func (e *Executor) permissionDecisionForRule(dec permission.Decision, tc core.ToolCallItem, predictedReq *core.PermissionRequest) permissionDecision {
	switch dec.Effect {
	case permission.EffectDeny:
		reason := "denied by permission rule"
		if dec.Rule.Tool != "" {
			reason = "denied by rule " + dec.Rule.Tool + "(" + dec.Rule.Specifier + ")"
		}
		return permissionDecision{block: true, reason: reason}
	case permission.EffectAsk:
		// Full-takeover mode: asks become silent allows. Deny rules above
		// still block — they are hard rules, not approvals.
		if e.FullAccess() {
			return permissionDecision{}
		}
		// Only skip the basic prompt when (a) the engine fell through to its
		// default effect (no explicit rule matched — dec.Rule.Tool == ""),
		// and (b) a predicted capability request is already covered by a grant.
		// Otherwise the user's explicit command-level ask must be honored.
		if predictedReq != nil && dec.Rule.Tool == "" && !dec.Assessment.RequiresApproval() &&
			e.permissionAllowed(tc.Name, *predictedReq) {
			return permissionDecision{}
		}
		return permissionDecision{
			prompt:      true,
			promptTool:  tc.Name,
			promptArgs:  tc.Args,
			matchedRule: dec.Rule,
			approval:    approvalContextFromAssessment(dec.Assessment),
		}
	default:
		return permissionDecision{}
	}
}

func approvalContextFromAssessment(assessment permission.CallAssessment) *protocol.ApprovalContext {
	if !assessment.RequiresApproval() {
		return nil
	}
	context := &protocol.ApprovalContext{
		Risk:       assessment.Reason,
		Reason:     assessment.Reason,
		Structures: append([]string(nil), assessment.Signals...),
		Scope:      protocol.ApprovalScopeProject,
	}
	if slices.Contains(assessment.Signals, string(permission.ShellUnparseable)) {
		context.Scope = protocol.ApprovalScopeOnce
	}
	return context
}

func shellCommandForPolicy(tc core.ToolCallItem) (string, bool) {
	if tc.Name == "shell" {
		cmd, _ := tc.Args["command"].(string)
		cmd = strings.TrimSpace(cmd)
		return cmd, cmd != ""
	}
	return "", false
}

// permissionEngine returns the configured engine plus workspace/home. The
// default engine is created by NewExecutor and contains builtin rules.
func (e *Executor) permissionEngine() (*permission.Engine, string, string) {
	e.fnMu.RLock()
	defer e.fnMu.RUnlock()
	return e.permEngine, e.permWorkspace, e.permHome
}

// defaultPermissionEffect is the fallback effect when no rule matches. The
// Shell is evaluated through command-level shell(...) rules before this
// fallback. Process management defaults to allow because it starts no command.
// Delegated calls are evaluated separately with an ask fallback because the
// runtime cannot make a safety claim about remote capabilities. All ordinary
// non-shell tools default to allow.
func defaultPermissionEffect(toolName string) permission.Effect {
	if toolName == "shell" {
		return permission.EffectAsk
	}
	return permission.EffectAllow
}

// promptConfirm builds and issues a confirm request for a tool call that the
// permission engine decided needs a prompt.
// Returns (reply, true) if the user allowed, (zero, false) on deny/no-fn.
//
// Predicted capabilities are merged into this request so one decision covers
// the command and the exact capability scope shown to the user.
func (e *Executor) promptConfirm(tc core.ToolCallItem, confirmFn protocol.ConfirmFunc, predictedReq *core.PermissionRequest, dec permissionDecision) (protocol.ConfirmReply, bool) {
	if confirmFn == nil {
		return protocol.ConfirmReply{}, false
	}
	toolName := dec.promptTool
	if toolName == "" {
		toolName = tc.Name
	}
	args := dec.promptArgs
	if args == nil {
		args = tc.Args
	}
	approval := dec.approval.Clone()
	if predictedReq != nil {
		predicted := approvalContextFromPermission(*predictedReq)
		if approval == nil {
			approval = predicted
		} else {
			approval.Reason = predicted.Reason
			approval.Capabilities = predicted.Capabilities
			approval.Scope = predicted.Scope
			approval.Workspace = predicted.Workspace
			approval.Sandbox = predicted.Sandbox
			approval.WritePaths = predicted.WritePaths
		}
		approval.Combined = true
	}
	req := protocol.NewApprovalRequest(toolName, protocol.ApprovalArgs(args), protocol.ConfirmKindPermission, approval)
	req.CallID = tc.ID
	reply := confirmFn(req)
	if !reply.Allowed {
		return reply, false
	}
	return reply, true
}

// rememberAllowRule persists an allow rule when the user approves an "ask"
// prompt with "remember". It derives the rule specifier from the tool call so
// future matching calls auto-approve, then rebuilds the engine immediately.
func (e *Executor) rememberAllowRule(toolName string, args map[string]any, matched permission.Rule) {
	e.fnMu.RLock()
	store := e.permStore
	ws := e.permWorkspace
	home := e.permHome
	engine := e.permEngine
	decl := e.permDecl
	e.fnMu.RUnlock()
	if store == nil || ws == "" || engine == nil {
		return
	}
	var rules []permission.Rule
	// Build a specifier from the call: shell uses the command prefix, file
	// tools use the path anchor. Fall back to the matched rule's specifier
	// (so a broad ask rule remembered becomes a broad allow).
	spec := matched.Specifier
	if toolName == "shell" {
		if cmd, _ := args["command"].(string); cmd != "" {
			rules = append(rules, bashRememberRules(toolName, cmd)...)
		}
	} else if p, _ := args["path"].(string); p != "" {
		spec = pathRememberSpec(p, ws, home)
	}
	if len(rules) == 0 {
		rules = append(rules, permission.Rule{Tool: toolName, Specifier: spec, Effect: permission.EffectAllow})
	}
	for _, rule := range rules {
		_ = store.RememberRule(ws, rule)
	}
	e.rebuildEngine(decl, store, ws)
}

func (e *Executor) callbacks() (protocol.PhaseFunc, protocol.ConfirmFunc, bool) {
	e.fnMu.RLock()
	defer e.fnMu.RUnlock()
	return e.phaseFn, e.confirmFn, e.planMode
}

func (e *Executor) callTool(ctx context.Context, tool core.Tool, tc core.ToolCallItem) (output string, execErr error) {
	defer func() {
		if r := recover(); r != nil {
			execErr = fmt.Errorf("panic: %v", r)
		}
	}()
	return tool.Execute(e.toolContext(ctx), tc.Args)
}

func (e *Executor) toolContext(ctx context.Context) context.Context {
	return workspace.WithManager(execution.WithExecutionState(ctx, e.state), e.workspace)
}

func (e *Executor) invalidateMutatedPaths(toolName string, paths []string) {
	if toolName != "write" && toolName != "edit" {
		return
	}
	for _, p := range paths {
		if resolved, err := resolvePath(p); err == nil {
			if cache := e.state.FileCache; cache != nil {
				cache.Invalidate(resolved)
			}
		}
	}
}
