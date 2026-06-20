package governance

import "nekocode/bot/agent/ledger"

func (g *Manager) CheckFinalAnswer(text string) []ledger.FinalIssue {
	if g.Ledger == nil {
		return nil
	}
	return ledger.CheckFinalAnswer(text, g.Ledger.Snapshot())
}
