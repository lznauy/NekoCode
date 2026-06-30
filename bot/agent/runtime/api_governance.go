package runtime

import (
	"nekocode/bot/hooks"
	aggov "nekocode/bot/policy"
)

func (a *Agent) SetGovernanceManager(gov *aggov.Manager) {
	a.deps.gov = gov
}

func (a *Agent) GovernanceManager() *aggov.Manager {
	return a.deps.gov
}

// SetHookRegistry wires the hook registry into the agent's govManager.
// If no manager exists yet, one is created.
func (a *Agent) SetHookRegistry(m *hooks.Registry) {
	if a.deps.gov == nil {
		a.deps.gov = aggov.NewManager(m)
	} else {
		a.deps.gov.HookReg = m
	}
}
