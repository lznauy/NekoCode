package runner

import (
	"context"
	"errors"
	"net/url"
	"slices"
	"strings"

	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/execution"
	"nekocode/bot/extension/tool/runtime/permission"
	"nekocode/bot/extension/tool/runtime/workspace"
	"nekocode/logger"
	"nekocode/protocol"
	textutil "nekocode/util/text"
)

func (e *Executor) executeOne(ctx context.Context, tc core.ToolCallItem) (result core.ToolCallResult) {
	evidence := protocol.ToolEvidence{Schema: 1}
	stage := "lookup"
	var invocationErr error
	invoke := func(callCtx context.Context, run func() (core.ToolOutput, error)) (core.ToolOutput, error) {
		output, err, receipt := observeExecution(callCtx, tc, run)
		evidence.Executions = append(evidence.Executions, receipt)
		invocationErr = errors.Join(invocationErr, err)
		return output, err
	}
	defer func() {
		result.Cause = invocationErr
		if len(evidence.Executions) > 0 {
			evidence.ReturnedHash = core.OutputHash(result.TextOutput())
			result.Evidence = evidence
		} else {
			result.NonExecution = core.NotExecuted(ctx, tc, stage, result.Error).NonExecution
		}
	}()

	defer func() {
		bounded, err := e.state.OutputStore.BoundOutput(result.TextOutput(), core.MaxOutputBytes)
		if err == nil && bounded.Artifact.Path != "" {
			err = e.AllowOutputFile(bounded.Artifact.Path)
		}
		if err != nil {
			evidence.PreservationFailed = true
			result.Error = "cannot preserve tool result: " + err.Error()
			result.Output = ""
			result.Artifact = protocol.ToolOutputArtifact{}
			result.Process.OutputComplete = false
			return
		}
		evidence.Bounded = bounded.Text != result.TextOutput().Text || bounded.Artifact != result.Artifact
		normalized := textutil.NormalizeTerminalOutput(bounded.Text)
		evidence.Normalized = normalized != bounded.Text
		bounded.Text = normalized
		result.Artifact = bounded.Artifact
		result.Process = bounded.Process
		if result.Error != "" {
			result.Error, result.Output = bounded.Text, ""
		} else {
			result.Output = bounded.Text
		}
	}()
	if err := ctx.Err(); err != nil {
		stage = "dispatch"
		return core.ToolCallResult{ID: tc.ID, Name: tc.Name, Error: err.Error()}
	}
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

	phaseFn, confirmFn := e.callbacks()
	confirmFn = e.observeConfirm(ctx, confirmFn)
	if phaseFn != nil {
		phaseFn(protocol.PhaseRunning + " " + resultName)
	}
	stage = "mode"
	if err := e.checkToolPolicy(tc.Name, entry); err != nil {
		return core.ToolCallResult{ID: tc.ID, Name: resultName, Error: err.Error()}
	}

	tc = e.normalizeFilePolicyPath(tc)
	var errMsg string
	var ok bool
	workspaceChecked := false
	stage = "workspace"
	if tc.Name == "image_understand" {
		// Its permission planner hashes the file for approval binding, so first
		// enforce a strictly lexical workspace check. Other file tools stay below
		// permission evaluation: resolving their paths first could touch a denied
		// Windows UNC path and leak DNS/SMB traffic before the deny takes effect.
		if tc, errMsg, ok = e.ensureWorkspaceAccess(tc, confirmFn); !ok {
			return core.ToolCallResult{ID: tc.ID, Name: resultName, Error: errMsg}
		}
		workspaceChecked = true
	}

	// Predict the escalation request a privileged tool will raise from explicit
	// sandbox arguments. This lets the command approval dialog offer a single
	// unified decision without exposing legacy capability args.
	// If a grant already matches, skip the engine's default "ask" prompt.
	stage = "permission"
	predictedReq := e.permissionRequestFromEntry(entry, tc.Args)
	var preApproval *escalationApproval
	dec := e.evaluatePermission(ctx, tc, predictedReq)
	if dec.block {
		e.observePermissionDenial(ctx, tc, dec.reason, predictedReq)
		return core.ToolCallResult{ID: tc.ID, Name: resultName, Error: dec.reason}
	}

	// Resolve authority only after the lexical rule check, then evaluate the
	// actual target before displaying or saving a command approval.
	stage = "workspace"
	policyCall := tc
	var boundFileTarget []boundFileTarget
	if !workspaceChecked {
		previousPath := fileToolPath(tc)
		if tc, errMsg, ok = e.ensureWorkspaceAccess(tc, confirmFn); !ok {
			return core.ToolCallResult{ID: tc.ID, Name: resultName, Error: errMsg}
		}
		var targetErr error
		policyCall, boundFileTarget, targetErr = bindFilePolicyCall(tc)
		if targetErr != nil {
			return core.ToolCallResult{ID: tc.ID, Name: resultName, Error: targetErr.Error()}
		}
		if fileToolPath(policyCall) != previousPath {
			lexical := dec
			dec = e.evaluatePermission(ctx, policyCall, predictedReq)
			if dec.block {
				e.observePermissionDenial(ctx, tc, dec.reason, predictedReq)
				return core.ToolCallResult{ID: tc.ID, Name: resultName, Error: dec.reason}
			}
			// Explicit asks on the lexical alias remain mandatory, even when
			// the resolved target has an allow rule. Show the executed path.
			if lexical.prompt && lexical.mandatoryAsk {
				lexical.promptArgs = policyCall.Args
				lexical.policy = dec.policy
				dec = lexical
			}
		}
	}
	stage = "permission"
	if dec.prompt {
		reply, ok := e.promptConfirm(policyCall, confirmFn, predictedReq, dec)
		if !ok {
			if reply.err != nil {
				return core.ToolCallResult{ID: tc.ID, Name: resultName, Error: reply.err.Error()}
			}
			// The user explicitly declined. A bare "cancelled" reads like a
			// transient hiccup and invites the model to retry the very same
			// call — popping the dialog again. Name the decision instead.
			return core.ToolCallResult{ID: tc.ID, Name: resultName, Error: "permission denied by user; do not retry the same request — change your approach or ask the user how to proceed"}
		}
		if err := validateFileTargetBinding(boundFileTarget); err != nil {
			return core.ToolCallResult{ID: tc.ID, Name: resultName, Error: err.Error()}
		}
		if reply.Remember {
			if err := e.rememberApprovedRules(reply.policy, reply.rules); err != nil {
				return core.ToolCallResult{ID: tc.ID, Name: resultName, Error: "cannot save permanent approval; command was not executed: " + err.Error() + ". Fix the permissions file or choose allow once; do not retry automatically."}
			}
		}
		if predictedReq != nil {
			preApproval = &escalationApproval{remember: reply.Remember, predicted: predictedReq, policy: &reply.policy}
		}
	}

	stage = "checkpoint"
	if dec.policy != nil && !dec.policy.current(e) {
		return core.ToolCallResult{ID: tc.ID, Name: resultName, Error: errApprovalContextChanged.Error()}
	}
	if err := validateFileTargetBinding(boundFileTarget); err != nil {
		return core.ToolCallResult{ID: tc.ID, Name: resultName, Error: err.Error()}
	}
	captured, captureErr := e.captureMutationPaths(tc)
	if captureErr != nil {
		e.finalizeMutationPaths(captured)
		return core.ToolCallResult{ID: tc.ID, Name: resultName, Error: captureErr.Error()}
	}
	defer e.finalizeMutationPaths(captured)

	stage = "dispatch"
	if err := ctx.Err(); err != nil {
		return core.ToolCallResult{ID: tc.ID, Name: resultName, Error: err.Error()}
	}
	if err := validateFileTargetBinding(boundFileTarget); err != nil {
		return core.ToolCallResult{ID: tc.ID, Name: resultName, Error: err.Error()}
	}
	paths := toolPaths(tc)
	if dec.policy != nil && !dec.policy.current(e) {
		return core.ToolCallResult{ID: tc.ID, Name: resultName, Error: errApprovalContextChanged.Error()}
	}
	output, execErr := invoke(ctx, func() (core.ToolOutput, error) { return tool.Execute(e.toolContext(ctx), tc.Args) })
	e.noteExecutedShell(tc)
	if execErr != nil {
		preApproved := escalationApproval{policy: dec.policy}
		if preApproval != nil {
			preApproved = *preApproval
		}
		preApproved.fileTargets = boundFileTarget
		privileged := entry.Privileged
		if original := privileged; original != nil {
			privileged = func(callCtx context.Context, args map[string]any, req core.PermissionRequest) (core.ToolOutput, error) {
				if err := validateFileTargetBinding(boundFileTarget); err != nil {
					return core.ToolOutput{}, err
				}
				if dec.policy != nil && !dec.policy.current(e) {
					return core.ToolOutput{}, errApprovalContextChanged
				}
				return invoke(callCtx, func() (core.ToolOutput, error) { return original(callCtx, args, req) })
			}
		}
		escOut, escOk, escReason, escCause := e.tryPermissionEscalation(ctx, privileged, tc, execErr, confirmFn, preApproved, preApproval != nil, dec.jevSafe)
		if escOk {
			e.invalidateMutatedPaths(tc.Name, paths)
			return core.ToolCallResult{ID: tc.ID, Name: resultName, Output: escOut.Text, Artifact: escOut.Artifact, ReadID: escOut.ReadID, Process: escOut.Process}
		}
		if escReason == failRetryError {
			output = escOut
		}
		failure := permissionFailureMessage(tc, execErr, escReason, escCause)
		if output.Text != "" {
			failure += "\nOutput:\n" + output.Text
		}
		return core.ToolCallResult{ID: tc.ID, Name: resultName, Error: failure, Artifact: output.Artifact, Process: output.Process}
	}

	e.invalidateMutatedPaths(tc.Name, paths)
	return core.ToolCallResult{ID: tc.ID, Name: resultName, Output: output.Text, Artifact: output.Artifact, ReadID: output.ReadID, Process: output.Process}
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
	mandatoryAsk bool
	block        bool
	reason       string
	prompt       bool
	promptTool   string
	promptArgs   map[string]any
	approval     *protocol.ApprovalContext
	policy       *permissionSnapshot
	// jevSafe records that the auto-mode risk judge vetted this call's
	// content and no rule prompted — the same verdict that silently passes
	// the pre-execution check. The privileged escalation consults it so a
	// judge-approved command does not hit a second, redundant approval
	// dialog just for its capability grant.
	jevSafe bool
}

// evaluatePermission runs the rule engine and either blocks, prompts, or
// allows the call. If no explicit rule matched (engine fell to its default
// effect) AND a predicted capability grant already covers the call, we skip
// the basic prompt. Explicit ask rules (rm *, git push *, ...) always win:
// they are command-level safety prompts the user opted into, orthogonal to
// capability grants, so a net.outbound grant MUST NOT silence an "ask rm *".
func (e *Executor) evaluatePermission(ctx context.Context, tc core.ToolCallItem, predictedReq *core.PermissionRequest) (decision permissionDecision) {
	policy := e.permissionSnapshot()
	defer func() { decision.policy = &policy }()
	engine, ws, home := policy.engine, policy.workspace, policy.home
	if cmd, ok := shellCommandForPolicy(tc); ok {
		if decision, hit := e.evaluateAs(ctx, engine, ws, home, "shell", map[string]any{"command": cmd}, tc, predictedReq, permission.EffectAsk); hit {
			return decision
		}
	}

	// Delegating tools are evaluated against the effective target attached to
	// their registry entry. The runner does not know concrete proxy protocols.
	if tc.EffectiveName != "" {
		if decision, hit := e.evaluateAs(ctx, engine, ws, home, tc.EffectiveName, tc.EffectiveArgs, tc, predictedReq, permission.EffectAsk); hit {
			return decision
		}
	}

	callInfo := permission.BuildCallInfo(tc.Name, tc.Args, ws, home)
	dec := engine.EvaluateContext(ctx, tc.Name, callInfo, defaultPermissionEffect(tc.Name))
	decision = e.applyAutoWebGate(ctx, e.permissionDecisionForRule(dec, tc, predictedReq), tc)
	e.annotateJevVerdict(ctx, tc, dec.Assessment.Verdict, decision)
	decision.jevSafe = jevSafeVerdict(dec.Assessment.Verdict, decision)
	return decision
}

// jevSafeVerdict reports whether the auto-mode risk judge explicitly passed
// this call and no rule prompt/block overrode it — the condition the
// privileged escalation consults to skip a redundant capability dialog.
func jevSafeVerdict(verdict permission.JudgeVerdict, decision permissionDecision) bool {
	return verdict == permission.JudgeSafe && !decision.prompt && !decision.block
}

// annotateJevVerdict surfaces the judge result independently of tool output.
func (e *Executor) annotateJevVerdict(ctx context.Context, tc core.ToolCallItem, verdict permission.JudgeVerdict, decision permissionDecision) {
	if decision.block {
		return
	}
	if verdict == permission.JudgeSafe && !decision.prompt {
		e.emitJevNote(ctx, tc, protocol.ToolDecisionJevSafe)
	} else if verdict == permission.JudgeDangerous && decision.prompt {
		e.emitJevNote(ctx, tc, protocol.ToolDecisionJevDangerous)
	} else if verdict == permission.JudgeFailed {
		e.emitJevNote(ctx, tc, protocol.ToolDecisionJevFailed)
	}
}

// emitJevNote re-emits the tool's preview with a jev note appended, so every
// UI that renders tool entries shows what the auto-mode judge decided.
func (e *Executor) emitJevNote(ctx context.Context, tc core.ToolCallItem, decision protocol.ToolDecision) {
	e.fnMu.RLock()
	pfn := e.previewFn
	e.fnMu.RUnlock()
	if pfn == nil {
		return
	}
	preview, allowed := e.authorizedPreview(ctx, tc)
	if !allowed {
		return
	}

	// Keep trusted decision metadata out of both the tool-controlled preview
	// text and the shared execution arguments.
	pfn(tc.ID, tc.Name, tc.Args, preview, decision)
}

// noteExecutedShell reports executed shell commands to the registered hook so
// the risk judge's later verdicts carry session context.
func (e *Executor) noteExecutedShell(tc core.ToolCallItem) {
	if tc.Name != "bash" && tc.Name != "shell" {
		return
	}
	e.fnMu.RLock()
	hook := e.shellExecutedHook
	e.fnMu.RUnlock()
	if hook == nil {
		return
	}
	if cmd, _ := tc.Args["command"].(string); cmd != "" {
		hook(cmd)
	}
}

// webGateReason is the approval-dialog reason shown when the auto-mode URL
// gate forces a fetch back to the normal ask.
const webGateReason = "jev: URL judged risky by the risk judge"

// applyAutoWebGate is the auto-mode safety net for web fetches: a fetch the
// rules allow is still judged once, and a confidently-suspicious URL drops
// back to the normal ask. Judge unavailable or undecided keeps the rule's
// decision (fail open to the rule, fail closed to prompting only on a clear
// dangerous verdict). Full-takeover mode is untouched.
func (e *Executor) applyAutoWebGate(ctx context.Context, decision permissionDecision, tc core.ToolCallItem) permissionDecision {
	if decision.block || decision.prompt {
		return decision
	}
	e.fnMu.RLock()
	judge := e.webAutoJudge
	e.fnMu.RUnlock()
	if e.permissionMode() != permission.ModeAuto || judge == nil {
		return decision
	}
	if !isWebFetchTool(tc.Name) {
		return decision
	}
	url, _ := tc.Args["url"].(string)
	if url == "" {
		return decision
	}
	dangerous, ok := judge(ctx, url)
	if !ok {
		e.emitJevNote(ctx, tc, protocol.ToolDecisionJevURLUnavailable)
		return decision
	}
	if dangerous {
		e.emitJevNote(ctx, tc, protocol.ToolDecisionJevURLRisky)
		return permissionDecision{
			prompt:     true,
			promptTool: tc.Name,
			promptArgs: tc.Args,
			approval:   &protocol.ApprovalContext{Risk: webGateReason, Reason: webGateReason},
		}
	}
	// The gate consulted the judge and the URL passed: surface that too,
	// otherwise auto-mode URL approvals are indistinguishable from rule
	// allows.
	e.emitJevNote(ctx, tc, protocol.ToolDecisionJevURLSafe)
	return decision
}

// evaluateAs evaluates the engine under an effective tool identity (shell
// with its command, or a capability call's mcp__server__tool target). A
// block/prompt decision is annotated with that identity so the confirm
// dialog and remembered rules name the real tool, and hit is true; an
// allowed call reports hit=false so the caller can fall through.
func (e *Executor) evaluateAs(ctx context.Context, engine *permission.Engine, ws, home, name string, args map[string]any, tc core.ToolCallItem, predictedReq *core.PermissionRequest, fallback permission.Effect) (permissionDecision, bool) {
	callInfo := permission.BuildCallInfo(name, args, ws, home)
	dec := engine.EvaluateContext(ctx, name, callInfo, fallback)
	decision := e.permissionDecisionForRule(dec, tc, predictedReq)
	if !decision.block && !decision.prompt {
		// A concrete allow for the delegated target is final. Falling through
		// would evaluate the proxy name as well and could re-prompt for
		// "capability" even though mcp__server__tool was explicitly allowed.
		if dec.Effect == permission.EffectAllow {
			decision = e.applyAutoWebGate(ctx, decision, tc)
			e.annotateJevVerdict(ctx, tc, dec.Assessment.Verdict, decision)
			decision.jevSafe = jevSafeVerdict(dec.Assessment.Verdict, decision)
			return decision, true
		}
		return permissionDecision{}, false
	}
	e.annotateJevVerdict(ctx, tc, dec.Assessment.Verdict, decision)
	decision.promptTool = name
	decision.promptArgs = args
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
		// (b) no verdict reason is attached (a judge "dangerous" verdict must
		// still prompt even when a grant covers the call), and (c) a
		// predicted capability request is already covered by a grant.
		// Otherwise the user's explicit command-level ask must be honored.
		if predictedReq != nil && dec.Rule.Tool == "" && !dec.Assessment.RequiresApproval() &&
			dec.Assessment.Verdict == permission.JudgeNotRun && dec.Assessment.Reason == "" && e.permissionAllowed(tc.Name, *predictedReq) {
			return permissionDecision{}
		}
		approval := approvalContextFromAssessment(dec.Assessment)
		if approval == nil {
			approval = &protocol.ApprovalContext{Scope: protocol.ApprovalScopeProject}
		}
		switch {
		case dec.Rule.Tool != "":
			approval.Source = "user_rule"
			if dec.Rule.Source == "builtin" {
				approval.Source = "builtin_rule"
			}
			approval.Rule = dec.Rule.Tool + "(" + dec.Rule.Specifier + ")"
			if dec.Rule.Effect == permission.EffectAsk && dec.Rule.Source != "builtin" {
				approval.Scope = protocol.ApprovalScopeOnce
				approval.Reason = "explicit ask rule requires approval on every call"
			}
		case dec.Assessment.Verdict != permission.JudgeNotRun:
			approval.Source = "jev"
		case dec.Assessment.RequiresApproval():
			approval.Source = "shell_structure"
		default:
			approval.Source = "default_policy"
		}
		return permissionDecision{
			mandatoryAsk: dec.Rule.Tool != "" && dec.Rule.Effect == permission.EffectAsk && dec.Rule.Source != "builtin",
			prompt:       true,
			promptTool:   tc.Name,
			promptArgs:   tc.Args,
			approval:     approval,
		}
	default:
		return permissionDecision{}
	}
}

func approvalContextFromAssessment(assessment permission.CallAssessment) *protocol.ApprovalContext {
	// No signals and no reason means nothing to explain. A reason without
	// signals still surfaces: the auto-mode judge's verdict ("jev: judged
	// dangerous") is pure reason, and the dialog must show why it asked.
	if !assessment.RequiresApproval() && assessment.Reason == "" {
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
	policy := e.permissionSnapshot()
	return policy.engine, policy.workspace, policy.home
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
func (e *Executor) promptConfirm(tc core.ToolCallItem, confirmFn protocol.ConfirmFunc, predictedReq *core.PermissionRequest, dec permissionDecision) (commandApprovalReply, bool) {
	if confirmFn == nil {
		return commandApprovalReply{}, false
	}
	policy := e.permissionSnapshot()
	if dec.policy != nil {
		policy = *dec.policy
	}
	if !policy.current(e) {
		return commandApprovalReply{err: errApprovalContextChanged}, false
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
			if approval.Reason == "" {
				approval.Reason = predicted.Reason
			}
			approval.Capabilities = predicted.Capabilities
			if approval.Scope != protocol.ApprovalScopeOnce {
				approval.Scope = predicted.Scope
			}
			approval.Workspace = predicted.Workspace
			approval.Sandbox = predicted.Sandbox
			approval.WritePaths = predicted.WritePaths
			approval.OutboundDomains = predicted.OutboundDomains
		}
		approval.Combined = true
	}
	if approval == nil {
		approval = &protocol.ApprovalContext{Scope: protocol.ApprovalScopeProject}
	}
	canRemember := approval.CanRemember()
	var rememberRules []permission.Rule
	if canRemember {
		ws, home, engine := policy.workspace, policy.home, policy.engine
		approval.RememberWorkspace = ws
		if engine != nil && toolName == "shell" {
			command, _ := args["command"].(string)
			approval.ApprovedRules = nil
			for _, rule := range engine.ApprovedShellRules(toolName, command) {
				approval.ApprovedRules = append(approval.ApprovedRules, protocol.ApprovalRememberRule{Tool: rule.Tool, Specifier: rule.Specifier, Literal: rule.Literal})
			}
		}
		rememberRules = pendingRulesToRemember(engine, toolName, args, ws, home)
		approval.RememberRules = nil
		for _, rule := range rememberRules {
			approval.RememberRules = append(approval.RememberRules, protocol.ApprovalRememberRule{Tool: rule.Tool, Specifier: rule.Specifier, Literal: rule.Literal, Lifetime: string(rule.Lifetime)})
		}
	}
	if len(rememberRules) == 0 && len(approval.Capabilities) == 0 {
		approval.Scope = protocol.ApprovalScopeOnce
		canRemember = false
	}
	req := protocol.NewApprovalRequest(toolName, protocol.ApprovalArgs(args), protocol.ConfirmKindPermission, approval)
	req.CallID = tc.ID
	reply := confirmFn(req)
	if !canRemember {
		reply.Remember = false
	}
	approved := commandApprovalReply{ConfirmReply: reply, policy: policy, rules: rememberRules}
	if !reply.Allowed {
		return approved, false
	}
	if !policy.current(e) {
		approved.err = errApprovalContextChanged
		return approved, false
	}
	return approved, true
}

func pendingRulesToRemember(engine *permission.Engine, toolName string, args map[string]any, ws, home string) []permission.Rule {
	if engine != nil && toolName == "shell" {
		if command, _ := args["command"].(string); command != "" {
			return engine.PendingShellRules(toolName, command)
		}
	}
	return rulesToRemember(toolName, args, ws, home)
}

func rulesToRemember(toolName string, args map[string]any, ws, home string) []permission.Rule {
	var spec string
	switch {
	case strings.HasPrefix(toolName, "mcp__"):
		// MCPMatcher matches the full target identity, never its arbitrary args.
		spec = toolName
	case isWebFetchTool(toolName):
		if u, _ := args["url"].(string); u != "" {
			spec = "domain:" + urlHostForRemember(u)
		}
	default:
		switch toolName {
		case "read", "write", "edit", "list", "tree", "grep", "glob":
			if p, _ := args["path"].(string); p != "" {
				spec = pathRememberSpec(p, ws, home)
			}
		}
	}
	if spec == "" {
		return nil
	}
	return []permission.Rule{{Tool: toolName, Specifier: spec, Effect: permission.EffectAllow}}
}

// isWebFetchTool reports whether the tool is a URL-fetching builtin covered
// by the auto-mode web gate.
func isWebFetchTool(name string) bool {
	switch name {
	case "web_fetch", "webfetch", "web_extract":
		return true
	}
	return false
}

// urlHostForRemember extracts the hostname for a remembered domain rule.
func urlHostForRemember(raw string) string {
	raw = strings.TrimSpace(raw)
	if !strings.Contains(raw, "://") {
		if i := strings.IndexByte(raw, '/'); i > 0 {
			return raw[:i]
		}
		return raw
	}
	if u, err := url.Parse(raw); err == nil {
		return u.Hostname()
	}
	return raw
}

func (e *Executor) callbacks() (protocol.PhaseFunc, protocol.ConfirmFunc) {
	e.fnMu.RLock()
	phase := e.phaseFn
	e.fnMu.RUnlock()
	return phase, e.ConfirmFn()
}

func (e *Executor) toolContext(ctx context.Context) context.Context {
	return workspace.WithManager(core.WithOutputStore(execution.WithExecutionState(ctx, e.state), e.state.OutputStore), e.workspace)
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

// AllowOutputFile grants access to one generated artifact, never the shared
// directory containing other sessions' results.
func (e *Executor) AllowOutputFile(path string) error {
	_, err := e.workspace.AddSessionRoot(path, workspace.AccessReadOnly)
	return err
}
