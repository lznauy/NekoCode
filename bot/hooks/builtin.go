package hooks

func RegisterBuiltin(m *Manager) {
	m.Register(QuotaHook())
	m.Register(VerificationHook())
	m.Register(UnfinishedWorkHook())
	m.Register(ExplorationExhaustedHook())
	m.Register(ExplorationLowHook())
	m.Register(ExploreCascadeHook())
	m.Register(GarbledToolCallHook())
	m.Register(RepeatedToolCallHook())
	m.Register(GarbledCircuitBreaker())
}
