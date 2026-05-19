package hooks

func RegisterBuiltin(r *Registry) {
	r.AddInject("quota", QuotaHint())
	r.AddInject("verification", VerificationHint())
	r.AddInject("unfinished_work", UnfinishedWorkHint())
	r.AddInject("modified_files", ModifiedFilesHint())
	r.AddInject("exploration_exhausted", ExplorationExhaustedHint())
	r.AddInject("exploration_low", ExplorationLowHint())
	r.AddInject("explore_cascade", ExploreCascadeHint())
	r.AddInject("garbled_tool_call", GarbledToolCallHint())
	r.AddInject("repeated_tool_call", RepeatedToolCallHint())

	r.AddStop("garbled_circuit_breaker", GarbledCircuitBreaker())
}
