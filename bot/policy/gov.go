package policy

import (
	"nekocode/bot/hooks"
	"nekocode/bot/policy/budget"
	"nekocode/bot/policy/ledger"
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
