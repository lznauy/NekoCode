package runtime

import (
	aggov "nekocode/bot/agent/governance"
	"nekocode/bot/hooks"
)

type GovManager = aggov.Manager
type govManager = aggov.Manager
type ToolQuotaData = aggov.QuotaData
type ToolCallInfo = aggov.ToolCallInfo

func newGovManager(hookReg *hooks.Registry) *govManager {
	return aggov.NewManager(hookReg)
}

func NewGovernanceManager(hookReg *hooks.Registry) *GovManager {
	return aggov.NewManager(hookReg)
}
