package governance

import (
	"nekocode/bot/agent/budget"
	"nekocode/bot/agent/governance/ledger"
	"nekocode/bot/hooks"
)

type Manager struct {
	HookReg     *hooks.Registry
	Ledger      *ledger.Ledger
	Exploration *budget.ExplorationTracker

	prevReads         int
	prevModifies      int
	prevVerifications int
}

func NewManager(hookReg *hooks.Registry) *Manager {
	return &Manager{
		HookReg:     hookReg,
		Ledger:      ledger.New(),
		Exploration: budget.NewExplorationTracker(),
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
