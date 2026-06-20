package governance

import (
	"nekocode/bot/agent/budget"
	gatepkg "nekocode/bot/agent/gate"
	"nekocode/bot/agent/ledger"
	"nekocode/bot/hooks"
)

type GovManager = Manager
type ResponseGate = gatepkg.ResponseGate

type Manager struct {
	HookReg     *hooks.Registry
	Ledger      *ledger.Ledger
	Exploration *budget.ExplorationTracker
	Gate        *ResponseGate

	prevReads         int
	prevModifies      int
	prevVerifications int
}

func NewManager(hookReg *hooks.Registry) *Manager {
	return &Manager{
		HookReg:     hookReg,
		Ledger:      ledger.New(),
		Exploration: budget.NewExplorationTracker(),
		Gate:        gatepkg.NewResponseGate(),
	}
}

type QuotaData struct {
	MaxSlots int
	Used     int
}

type ToolCallInfo struct {
	Name   string
	Args   map[string]any
	Output string
	Error  string
}
