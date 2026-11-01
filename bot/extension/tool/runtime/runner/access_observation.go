package runner

import (
	"nekocode/bot/extension/tool/runtime/permission"
	"nekocode/protocol"
)

// AccessObservation samples this executor's actual mode source, published rule
// engine, project grants and workspace. It never invokes matchers or judges.
func (e *Executor) AccessObservation() protocol.AccessObservation {
	e.fnMu.RLock()
	engine, store, workspace, home := e.permEngine, e.permStore, e.permWorkspace, e.permHome
	plan, shellJudge, webJudge := e.planMode, e.bashAutoJudge != nil, e.webAutoJudge != nil
	e.fnMu.RUnlock()
	mode := "unknown"
	switch e.permissionMode() {
	case permission.ModeManual:
		mode = "manual"
	case permission.ModeAuto:
		mode = "auto"
	case permission.ModeFull:
		mode = "full"
	}
	grants, err := store.GrantsHash()
	return protocol.AccessObservation{
		Schema: 1, Mode: mode, PlanMode: plan,
		RulesHash: engine.RulesHash(workspace, home), GrantsHash: grants,
		WorkspaceHash: e.workspace.ConfigurationHash(), StoreUnavailable: err != nil,
		ShellJudgeConfigured: shellJudge, WebJudgeConfigured: webJudge,
	}
}
