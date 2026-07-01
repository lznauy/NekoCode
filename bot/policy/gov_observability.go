package policy

import (
	"nekocode/common/debug"
)

func (g *Manager) Summary() string {
	if g.Ledger == nil {
		return ""
	}
	snap := g.Ledger.Snapshot()
	hookPart := ""
	if g.HookReg != nil {
		hookPart = " | " + g.HookReg.HookCountsSnapshot().String()
	}
	return "Ledger: " + snap.Summary() + hookPart
}

func (g *Manager) GovernanceStats() string {
	if g.HookReg == nil {
		return ""
	}
	return " | " + g.HookReg.GovernanceStats()
}

func (g *Manager) LogSummary(steps int) {
	if g.Ledger == nil {
		return
	}
	snap := g.Ledger.Snapshot()
	hookStats := ""
	if g.HookReg != nil {
		hookStats = g.HookReg.GovernanceStats()
	}
	debug.Log("[GOVERNANCE] task complete: steps=%d, %s%s",
		steps, snap.Summary(), hookStats)
}
