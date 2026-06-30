package builtin

func All() []Hook {
	return []Hook{
		QuotaHook(),
		ToolResultGuardrailHook(),
		ReadBeforeWriteHook(),
		ReadOnlySpiralHook(),
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
