package runtime

import (
	"nekocode/bot/agent/ledger"
	"nekocode/bot/hooks"
)

func (a *Agent) applyFinalCheck(reasoning *ReasoningResult) bool {
	if a.gov == nil || a.gov.Ledger == nil {
		return false
	}
	issues := a.gov.CheckFinalAnswer(reasoning.ActionInput)
	if len(issues) == 0 {
		return false
	}
	msg := ledger.FormatIssues(issues)
	a.lastText = reasoning.ActionInput

	if a.gov.Gate == nil {
		a.gov.Gate = NewResponseGate()
	}
	retry, hint := a.gov.Gate.TryRetry(msg)
	if !retry {
		return false
	}
	a.injectHint(&hooks.Hint{Type: "final_check", Severity: "critical", Content: hint})
	a.step++
	return true
}

func (a *Agent) applyFinalPolicyBlock(reasoning *ReasoningResult, reason string) bool {
	if reason == "" {
		reason = "final answer blocked by policy"
	}
	a.lastText = reasoning.ActionInput

	if a.gov == nil || a.gov.Gate == nil {
		a.gov.Gate = NewResponseGate()
	}
	retry, hint := a.gov.Gate.TryRetry(reason)
	if !retry {
		return false
	}
	a.injectHint(&hooks.Hint{Type: "policy_block", Severity: "critical", Content: hint})
	a.step++
	return true
}
