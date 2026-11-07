package runner

import (
	"context"
	"fmt"
	"nekocode/bot/extension/tool/runtime/netpolicy"
	"nekocode/mode"
	"nekocode/protocol"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"

	tools "nekocode/bot/extension/tool"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/execution"
	"nekocode/bot/extension/tool/runtime/permission"
	"nekocode/bot/extension/tool/runtime/workspace"
)

type Executor struct {
	registry           *tools.Registry
	workspace          *workspace.Manager
	state              *execution.ExecutionState
	confirmFn          protocol.ConfirmFunc
	confirmSource      func() protocol.ConfirmFunc
	permissionObserver func(context.Context, PermissionEvent)
	phaseFn            protocol.PhaseFunc
	interactionMode    mode.Snapshot
	// Mode state is published atomically for delegated executors. Explicit
	// deny rules apply in every mode.
	modeState  atomic.Pointer[permissionModeState]
	modeSource func() permissionModeState
	// bashAuto is the "auto" permission mode: shell calls that match no rule
	// are judged by the optional risk judge — confidently safe commands run
	// without a prompt, everything else asks. Needs bashAutoJudge set.
	bashAutoJudge func(ctx context.Context, command string) (dangerous bool, ok bool)
	// webAutoJudge backs the auto-mode URL gate: before silently fetching a
	// URL the rules allow, the executor consults it and forces the normal
	// ask on a confidently-suspicious verdict. nil disables the gate.
	webAutoJudge func(ctx context.Context, url string) (dangerous bool, ok bool)
	// shellExecutedHook observes every shell command that actually ran,
	// giving the risk judge session context for later judgments.
	shellExecutedHook func(command string)
	planTools         map[string]struct{}
	previewFn         func(callID, toolName string, args map[string]any, preview string, decision protocol.ToolDecision)
	permStore         *permission.Store
	fnMu              sync.RWMutex
	// Permission rule engine (claude-code style allow/ask/deny). The engine's
	// deny→ask→allow decision is the single authority for whether a tool call
	// runs, prompts, or is blocked.
	permEngine *permission.Engine
	permDecl   permission.PermissionsDecl
	// permSession holds session-scoped grants (dynamic shell statements and
	// other one-off approvals). They never reach the permissions file and are
	// dropped with the executor.
	permSession         []permission.Rule
	permSessionRevision uint64
	permWorkspace       string
	permHome            string
	permRevision        uint64
	maxConcurrentTasks  int
}

// SetMaxConcurrentTasks configures delegated execution; nonpositive uses default.
func (e *Executor) SetMaxConcurrentTasks(limit int) {
	e.fnMu.Lock()
	e.maxConcurrentTasks = limit
	e.fnMu.Unlock()
}

func NewExecutor(r *tools.Registry) *Executor {
	var manager *workspace.Manager
	if r != nil && r.Workspace() != nil {
		manager = r.Workspace()
	} else {
		manager = workspace.New("", nil)
	}
	root := manager.Primary()
	e := &Executor{
		registry:      r,
		workspace:     manager,
		state:         execution.NewExecutionState(),
		permStore:     permission.NewStore(root),
		permWorkspace: root,
	}
	e.rebuildEngine()
	return e
}

// autoJudgeDecide adapts the optional risk judge for the permission engine:
// auto mode on + confidently-safe verdict → allow without a prompt; judge
// unavailable, uncertain, or dangerous → ask (fail closed).
func (e *Executor) autoJudgeDecide(ctx context.Context, command string) (allow bool, decided bool) {
	e.fnMu.RLock()
	judge := e.bashAutoJudge
	e.fnMu.RUnlock()
	if e.permissionMode() != permission.ModeAuto || judge == nil {
		return false, false
	}
	dangerous, ok := judge(ctx, command)
	if !ok {
		return false, false
	}
	return !dangerous, true
}

// SetBashAuto toggles the "auto" permission mode for shell calls. It is a
// thin alias kept for tests; production code drives modes through
// SetPermissionMode so the transition rules live in Bot.SetFullAccess /
// Bot.SetBashAuto only.
func (e *Executor) SetBashAuto(on bool) {
	e.fnMu.RLock()
	mode := e.localPermissionModeState().mode
	e.fnMu.RUnlock()
	if on {
		e.SetPermissionMode(permission.ModeAuto)
	} else if mode == permission.ModeAuto {
		e.SetPermissionMode(permission.ModeManual)
	}
}

// SetBashAutoJudge installs the risk judge backing the auto mode. ctx is the
// calling tool's context; a remote judge must honor its cancellation.
func (e *Executor) SetBashAutoJudge(fn func(ctx context.Context, command string) (dangerous bool, ok bool)) {
	e.fnMu.Lock()
	e.bashAutoJudge = fn
	e.fnMu.Unlock()
}

// SetWebAutoJudge installs the risk judge backing the auto-mode URL gate.
func (e *Executor) SetWebAutoJudge(fn func(ctx context.Context, url string) (dangerous bool, ok bool)) {
	e.fnMu.Lock()
	e.webAutoJudge = fn
	e.fnMu.Unlock()
}

// SetShellExecutedHook registers an observer fired after each shell command
// execution attempt (successful or not), so the risk judge's "recently
// executed" context only contains commands that actually ran.
func (e *Executor) SetShellExecutedHook(fn func(command string)) {
	e.fnMu.Lock()
	e.shellExecutedHook = fn
	e.fnMu.Unlock()
}

// ConfigureChildPermissions installs the parent's permission policy on a fresh
// delegated executor. Callbacks are copied under lock; the child owns its engine.
// Mode changes remain live so switching to manual also revokes delegated auto/full.
func (e *Executor) ConfigureChildPermissions(child *Executor) {
	e.fnMu.RLock()
	shell, web, hook := e.bashAutoJudge, e.webAutoJudge, e.shellExecutedHook
	decl, store, root, home := e.permDecl, e.permStore, e.permWorkspace, e.permHome
	// Flatten inheritance to an atomic source: snapshots holding a child lock
	// must never acquire a parent executor lock.
	source := e.modeSource
	if source == nil {
		source = e.localPermissionModeState
	}
	e.fnMu.RUnlock()
	child.fnMu.Lock()
	child.modeSource = source
	child.fnMu.Unlock()
	child.SetBashAutoJudge(shell)
	child.SetWebAutoJudge(web)
	child.SetShellExecutedHook(hook)
	child.fnMu.Lock()
	child.permStore = store
	child.fnMu.Unlock()
	child.SetPermissionPolicy(decl, root, home)
}

func (e *Executor) ExecutionState() *execution.ExecutionState { return e.state }

func (e *Executor) SetConfirmFn(fn protocol.ConfirmFunc) {
	e.fnMu.Lock()
	e.confirmFn = fn
	e.confirmSource = nil
	e.fnMu.Unlock()
}

// SetConfirmSource resolves an optional live handler without fabricating a
// denial callback when the application has no approval surface.
func (e *Executor) SetConfirmSource(source func() protocol.ConfirmFunc) {
	e.fnMu.Lock()
	e.confirmSource = source
	e.fnMu.Unlock()
}

func (e *Executor) ConfirmFn() protocol.ConfirmFunc {
	e.fnMu.RLock()
	source, fn := e.confirmSource, e.confirmFn
	e.fnMu.RUnlock()
	if source != nil {
		return source()
	}
	return fn
}

func (e *Executor) SetPhaseFn(fn protocol.PhaseFunc) {
	e.fnMu.Lock()
	e.phaseFn = fn
	e.fnMu.Unlock()
}

func (e *Executor) SetPlanMode(on bool) {
	e.fnMu.Lock()
	id := mode.Standard
	if on {
		id = mode.Plan
	}
	d, _ := mode.Resolve(id)
	e.interactionMode = d.Snapshot
	e.fnMu.Unlock()
}

// SetInteractionMode installs a validated, immutable run policy. Permission
// modes such as full access cannot relax its tool restrictions.
func (e *Executor) SetInteractionMode(snapshot mode.Snapshot) error {
	if _, err := mode.Validate(snapshot); err != nil {
		return err
	}
	e.fnMu.Lock()
	e.interactionMode = snapshot
	e.fnMu.Unlock()
	return nil
}

// SetFullAccess toggles the full-takeover permission mode. When on, every
// approval prompt (permission asks and capability escalations) is bypassed
// and the call runs immediately; explicit deny rules still block.
func (e *Executor) SetFullAccess(on bool) {
	e.fnMu.Lock()
	defer e.fnMu.Unlock()
	state := e.localPermissionModeState()
	if on {
		e.setPermissionModeLocked(permission.ModeFull)
	} else if state.mode == permission.ModeFull {
		e.setPermissionModeLocked(permission.ModeManual)
	}
}

// FullAccess reports whether the full-takeover permission mode is active.
func (e *Executor) FullAccess() bool {
	return e.permissionMode() == permission.ModeFull
}

// Each immutable state publishes mode and epoch together. A mode change away
// and back invalidates approvals in every delegated generation.
type permissionModeState struct {
	mode  permission.Mode
	epoch uint64
}

func (e *Executor) localPermissionModeState() permissionModeState {
	if state := e.modeState.Load(); state != nil {
		return *state
	}
	return permissionModeState{}
}

// permissionModeStateLocked only reads atomic state after choosing the source.
// ConfigureChildPermissions flattens the source, so this cannot acquire parent locks.
func (e *Executor) permissionModeStateLocked() permissionModeState {
	if e.modeSource != nil {
		return e.modeSource()
	}
	return e.localPermissionModeState()
}

func (e *Executor) permissionMode() permission.Mode {
	e.fnMu.RLock()
	defer e.fnMu.RUnlock()
	return e.permissionModeStateLocked().mode
}

func (e *Executor) setPermissionModeLocked(mode permission.Mode) {
	state := e.localPermissionModeState()
	if state.mode == mode {
		return
	}
	e.permRevision++
	e.modeState.Store(&permissionModeState{mode: mode, epoch: state.epoch + 1})
}

func (e *Executor) SetPermissionMode(mode permission.Mode) {
	e.fnMu.Lock()
	defer e.fnMu.Unlock()
	e.setPermissionModeLocked(mode)
}

// SetPlanTools replaces the tools allowed while plan mode is active. Registry
// declarations are used when this method is not called.
func (e *Executor) SetPlanTools(names ...string) {
	allowed := make(map[string]struct{}, len(names))
	for _, name := range names {
		if name != "" {
			allowed[name] = struct{}{}
		}
	}
	e.fnMu.Lock()
	e.planTools = allowed
	e.fnMu.Unlock()
}

func (e *Executor) planAllows(name string, registered bool) bool {
	e.fnMu.RLock()
	allowed := e.planTools
	_, configured := allowed[name]
	e.fnMu.RUnlock()
	if allowed != nil {
		return configured
	}
	return registered
}

// SetPreviewFn retains the original preview callback API. New integrations
// that need trusted decision metadata should use SetPreviewDecisionFn.
func (e *Executor) SetPreviewFn(fn func(callID, toolName string, args map[string]any, preview string)) {
	if fn == nil {
		e.SetPreviewDecisionFn(nil)
		return
	}
	e.SetPreviewDecisionFn(func(callID, toolName string, args map[string]any, preview string, _ protocol.ToolDecision) {
		fn(callID, toolName, args, preview)
	})
}

// SetPreviewDecisionFn registers a preview callback with trusted structured
// decision metadata for interfaces such as the TUI.
func (e *Executor) SetPreviewDecisionFn(fn func(callID, toolName string, args map[string]any, preview string, decision protocol.ToolDecision)) {
	e.fnMu.Lock()
	e.previewFn = fn
	e.fnMu.Unlock()
}

func (e *Executor) SetPermissionStore(store *permission.Store) {
	e.fnMu.Lock()
	defer e.fnMu.Unlock()
	e.permStore = store
	e.permRevision++
	e.rebuildEngineLocked()
}

// SetProjectStore binds the executor to a per-trusted permission store at
// the trusted user permission directory. After this, the engine is rebuilt
// against that project's grants/rules. Subsequent Allow/Remember calls
// target that project file (no more cross-project leakage).
func (e *Executor) SetProjectStore(projectRoot string) {
	projectRoot = filepath.Clean(projectRoot)
	e.fnMu.Lock()
	defer e.fnMu.Unlock()
	e.permStore = permission.NewStore(projectRoot)
	e.permWorkspace = projectRoot
	e.permRevision++
	e.rebuildEngineLocked()
}

// SetPermissionPolicy configures the declarative permission rule engine.
// executeOne evaluates every tool call through this engine (deny→ask→allow).
// workspace/home are used to resolve path anchors in file-tool rules.
func (e *Executor) SetPermissionPolicy(decl permission.PermissionsDecl, workspace, home string) {
	e.fnMu.Lock()
	defer e.fnMu.Unlock()
	e.permDecl = decl
	e.permWorkspace = workspace
	e.permHome = home
	e.permRevision++
	e.rebuildEngineLocked()
}

// SetWorkspace updates the workspace used for path-anchor resolution and
// rebuilds the engine (e.g. after /cd). Safe to call when no policy is set.
func (e *Executor) SetWorkspace(workspace, home string) {
	e.fnMu.Lock()
	defer e.fnMu.Unlock()
	e.permWorkspace = workspace
	e.permHome = home
	e.permRevision++
	e.rebuildEngineLocked()
}

// rebuildEngine reconstructs the permission engine from decl + store +
// workspace. Malformed declared rules fail closed so a bad config cannot
// silently disable the permission engine.
func (e *Executor) rebuildEngine() {
	e.fnMu.Lock()
	defer e.fnMu.Unlock()
	e.rebuildEngineLocked()
}

// Serialize policy reads, engine construction and publication. A late rebuild
// must never overwrite a newer policy or a concurrently remembered grant.
func (e *Executor) rebuildEngineLocked() {
	if revision := e.workspace.SessionRevision(); revision != e.permSessionRevision {
		e.permSession = nil
		e.permSessionRevision = revision
	}
	eng, err := permission.NewEngineForWorkspace(e.permDecl, e.permStore, e.permWorkspace)
	if err != nil {
		fmt.Fprintf(os.Stderr, "permission: %v — blocking tool calls until config is fixed\n", err)
		eng = permission.NewEngine(permission.DefaultMatchers())
		eng.SetRules([]permission.Rule{{Tool: "*", Effect: permission.EffectDeny, Source: "config-error"}})
		e.permEngine = eng
		return
	}
	// Session grants are re-applied on every rebuild so a policy reload does
	// not silently drop what the user approved for this session. They are
	// scoped to the project they were granted in: switching projects (or
	// binding a different store) must not carry them across.
	if len(e.permSession) > 0 {
		scoped := e.permSession[:0:0]
		for _, rule := range e.permSession {
			if rule.Workspace == "" || rule.Workspace == e.permWorkspace {
				scoped = append(scoped, rule)
			}
		}
		if len(scoped) > 0 {
			rules := eng.Rules()
			rules = append(rules, scoped...)
			eng.SetRules(rules)
		}
	}
	// Re-wire on every rebuild so the auto-mode judge survives rule reloads.
	eng.SetAutoJudge(e.autoJudgeDecide)
	eng.SetAutoJudgeEnabled(func() bool { return e.permissionMode() == permission.ModeAuto })
	e.permEngine = eng
}

// applySessionRulesLocked records session-scoped grants in memory. They are
// never written to the trusted permission store: exact statements of dynamic
// shell units rarely match a second call, so persisting them would only grow
// the rule list without ever reusing it. Grants are stamped with the current
// workspace and deduplicated, so repeated approvals do not pile up.
func (e *Executor) applySessionRulesLocked(rules []permission.Rule) {
	for _, rule := range rules {
		if rule.Persistent() {
			continue
		}
		rule.Source = "session"
		rule.Workspace = e.permWorkspace
		duplicate := false
		for _, existing := range e.permSession {
			if existing.Tool == rule.Tool && existing.Specifier == rule.Specifier &&
				existing.Literal == rule.Literal && existing.Effect == rule.Effect &&
				existing.Workspace == rule.Workspace {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		e.permSession = append(e.permSession, rule)
	}
}

// SandboxEngine returns the current permission engine (or nil if not ready).
// Tools use it to look up sandbox rules (e.g. pnpm dev → network).
func (e *Executor) SandboxEngine() *permission.Engine {
	return e.permissionSnapshot().engine
}

type escalationApproval struct {
	fileTargets []boundFileTarget
	remember    bool
	policy      *permissionSnapshot
	// predicted is the capability request included in the unified dialog.
	// The pre-approval only covers escalation requests within this scope —
	// see requestCoveredBy.
	predicted *core.PermissionRequest
}

// requestCoveredBy reports whether an actual escalation request stays within
// the capabilities the user saw and approved. Without this check a tool
// could request broader capabilities at execution time (e.g. process.host
// when only net.outbound was predicted) and get them silently.
func (a escalationApproval) requestCoveredBy(req core.PermissionRequest) bool {
	if a.predicted == nil {
		return false
	}
	for _, cap := range req.Capabilities {
		if !slices.Contains(a.predicted.Capabilities, cap) {
			return false
		}
	}
	if slices.Contains(req.Capabilities, core.CapNetOutbound) {
		have, err := netpolicy.FromDetails(a.predicted.Details)
		if err != nil {
			return false
		}
		need, err := netpolicy.FromDetails(req.Details)
		if err != nil || !netpolicy.Covers(have, need) {
			return false
		}
	}
	return permission.ContainsAllWritePaths(
		permission.WritePathsFromRequest(*a.predicted),
		permission.WritePathsFromRequest(req),
	)
}
