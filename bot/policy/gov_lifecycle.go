package policy

import "nekocode/bot/hooks"

func (g *Manager) ResetTurnBetween(input string, quota QuotaData) {
	if g.HookReg == nil {
		return
	}
	g.HookReg.ResetTurn()
	g.HookReg.Set(hooks.StoreQuotaReads, int64(max(0, quota.MaxSlots-quota.Used)))
	g.HookReg.Set(hooks.StoreExploreScore, int64(g.Exploration.Score))
	g.HookReg.SetStr(hooks.StoreStepInput, input)
	g.HookReg.Set(hooks.StoreStepInputLen, int64(len([]rune(input))))
	g.SyncLedgerToHooks()
}

func (g *Manager) SyncLedgerToHooks() {
	if g.Ledger == nil || g.HookReg == nil {
		return
	}
	snap := g.Ledger.Snapshot()
	g.HookReg.Set(hooks.StoreLedgerModified, int64(len(snap.ModifiedFiles)))
	verified := int64(0)
	if snap.HasPassingVerification() {
		verified = 1
	}
	g.HookReg.Set(hooks.StoreLedgerVerified, verified)
	g.HookReg.Set(hooks.StoreLedgerErrors, int64(len(snap.ToolErrors)))
	g.HookReg.Set(hooks.StoreLedgerBlocked, int64(len(snap.BlockedTools)))
	nonDoc := int64(0)
	if snap.HasNonDocumentationModifications() {
		nonDoc = 1
	}
	g.HookReg.Set(hooks.StoreLedgerNonDocModified, nonDoc)

	curReads := len(snap.ReadFiles)
	curModifies := len(snap.ModifiedFiles)
	curVerifications := len(snap.Verifications)
	newReads := curReads - g.prevReads
	newModifies := curModifies - g.prevModifies
	newVerifications := curVerifications - g.prevVerifications

	if newReads > 0 || newModifies > 0 || newVerifications > 0 {
		g.HookReg.Set(hooks.StoreLedgerProgress, 1)
	} else {
		g.HookReg.Set(hooks.StoreLedgerProgress, 0)
	}
	g.prevReads = curReads
	g.prevModifies = curModifies
	g.prevVerifications = curVerifications
}

func (g *Manager) Reset() {
	if g.Exploration != nil {
		g.Exploration.Reset()
	}
	if g.Ledger != nil {
		g.Ledger.Reset()
	}
	if g.HookReg != nil {
		g.HookReg.ResetSession()
	}
	g.prevReads = 0
	g.prevModifies = 0
	g.prevVerifications = 0
}
