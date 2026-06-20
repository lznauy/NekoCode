package builtin

const (
	StoreToolResearcher = "turn:researcher"
	StoreQuotaReads     = "gauge:quota_reads"
	StoreExploreScore   = "gauge:explore"
	StoreTasksAllDone   = "gauge:tasks_done"
	StoreHasTasks       = "turn:has_tasks"
	StoreTurnToolCalls  = "turn:tool_calls"
	StoreStepInputLen   = "turn:step_len"
	StoreStepInput      = "value:step"
	StoreExploreCalls   = "counter:explore_calls"
	StoreHasEdits       = "turn:has_edits"
	StoreRespGarbled    = "counter:garbled"
	StoreLedgerModified = "gauge:ledger_modified"
	StoreLedgerVerified = "gauge:ledger_verified"
	StoreLedgerProgress = "turn:ledger_progress"

	CounterQuotaWarned     = "counter:quota_warned"
	CounterVerifyInjected  = "counter:verify_injected"
	CounterExploreInjected = "counter:explore_injected"
	CounterStallTurns      = "counter:stall_turns"
	CounterQualityWarned   = "counter:quality_warned"

	PolicyExploreExhausted = "policy:explore_exhausted"
)
