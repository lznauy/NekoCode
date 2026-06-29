package runtime

import (
	aggov "nekocode/bot/agent/governance"
	"nekocode/bot/agent/reasoning"
	gatepkg "nekocode/bot/agent/gate"
	subslotpkg "nekocode/bot/agent/subslot"
	"nekocode/bot/hooks"
)

// -- gate -------------------------------------------------------------------

type ResponseGate = gatepkg.ResponseGate

func NewResponseGate() *ResponseGate { return gatepkg.NewResponseGate() }

// -- governance -------------------------------------------------------------

type GovManager = aggov.Manager
type ToolQuotaData = aggov.QuotaData
type ToolCallInfo = aggov.ToolCallInfo

func NewGovernanceManager(hookReg *hooks.Registry) *GovManager {
	return aggov.NewManager(hookReg)
}

// -- reasoning --------------------------------------------------------------

func isGarbledToolCall(text string) bool { return reasoning.IsGarbledToolCall(text) }

// -- subslot ----------------------------------------------------------------

type SubSlotManager = subslotpkg.Manager

func NewSubSlotManager() *SubSlotManager { return subslotpkg.NewManager() }
