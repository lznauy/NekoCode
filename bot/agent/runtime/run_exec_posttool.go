package runtime

import "nekocode/bot/hooks"

func (a *Agent) applyPostToolHooks() (bool, hooks.StopReason) {
	if a.gov == nil || a.gov.HookReg == nil {
		return false, hooks.StopCompleted
	}
	for _, r := range a.gov.HookReg.Evaluate(hooks.PostTool, "", false) {
		if r.Stop != nil {
			a.stopReason = *r.Stop
			a.lastText = ""
			return true, *r.Stop
		}
		if r.RequireTool != nil {
			reason := r.RequireTool.Reason
			if r.RequireTool.Tool != "" {
				reason = "必须先调用 " + r.RequireTool.Tool + "：" + reason
			}
			a.injectHint(&hooks.Hint{Type: "require_tool", Severity: "critical", Content: reason})
		}
		if r.BlockFinal != nil {
			a.injectHint(&hooks.Hint{Type: "block_final", Severity: "critical", Content: r.BlockFinal.Reason})
		}
		a.injectHint(r.Hint)
	}
	return false, hooks.StopCompleted
}
