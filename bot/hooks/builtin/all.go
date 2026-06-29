package builtin

func All() []Hook {
	return []Hook{
		QuotaHook(),
		VerificationHook(),
		ExplorationExhaustedHook(),
		ExplorationGuardHook(),
		ExploreCascadeHook(),
		ProgressStallHook(),
		CompletionQualityHook(),
		GarbledCircuitBreaker(),
		FinalCheckHook(),
	}
}
