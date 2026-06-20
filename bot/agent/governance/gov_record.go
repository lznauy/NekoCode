package governance

import (
	"nekocode/bot/agent/ledger"
	semanticspkg "nekocode/bot/governance"
	"nekocode/bot/hooks"
)

func (g *Manager) RecordToolCall(tc ToolCallInfo, blocked bool, blockText string) {
	sem := semanticspkg.ClassifyToolCall(tc.Name, tc.Args)

	if g.Ledger != nil {
		g.Ledger.RecordTool(ledger.ToolEvent{
			Name:      tc.Name,
			Args:      tc.Args,
			Output:    tc.Output,
			Error:     tc.Error,
			Blocked:   blocked,
			BlockText: blockText,
			Semantics: sem,
		})
	}

	g.Exploration.RecordCall(tc.Name, tc.Args)
	if g.HookReg == nil {
		return
	}
	if sem.Exploratory {
		g.HookReg.Inc(hooks.StoreExploreCalls)
	}
	g.HookReg.Inc(hooks.StoreToolPrefix + tc.Name)
	g.HookReg.Inc(hooks.StoreTurnToolCalls)
	if sem.Mutating {
		g.HookReg.Set(hooks.StoreHasEdits, 1)
		g.HookReg.Set(hooks.PolicyExploreExhausted, 0)
	}
	if tc.Name == "task" {
		if t, _ := tc.Args["type"].(string); t == "researcher" {
			g.HookReg.Inc(hooks.StoreToolResearcher)
		}
	}
}
