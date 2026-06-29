package hooks

// Store keys used by builtin hooks and the agent loop.
const (
	StoreToolPrefix     = "counter:tool:" // + name
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
	StoreLedgerModified       = "gauge:ledger_modified"
	StoreLedgerVerified       = "gauge:ledger_verified"
	StoreLedgerErrors         = "gauge:ledger_errors"
	StoreLedgerBlocked        = "gauge:ledger_blocked"
	StoreLedgerNonDocModified = "gauge:ledger_nondoc_modified" // 1 if non-documentation files were modified
	StoreLedgerProgress       = "turn:ledger_progress"          // 1 if this turn added new evidence
	StoreFinalAnswerText      = "value:final_answer"            // current turn's assistant final-answer text
	StoreSessionStarted       = "session:started"

	CounterQuotaWarned     = "counter:quota_warned"
	CounterVerifyInjected  = "counter:verify_injected"
	CounterExploreInjected = "counter:explore_injected"
	CounterStallTurns      = "counter:stall_turns"
	CounterQualityWarned   = "counter:quality_warned"

	PolicyExploreExhausted = "policy:explore_exhausted"
)
