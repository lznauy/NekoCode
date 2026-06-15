package hooks

// Store keys used by builtin hooks and the agent loop.
const (
	StoreToolPrefix     = "counter:tool:" // + name
	StoreToolResearcher = "turn:researcher"
	StoreFileModified   = "counter:modified" // persist across turns (reset only by ResetSession)
	StoreQuotaReads     = "gauge:quota_reads"
	StoreExploreScore   = "gauge:explore"
	StoreTasksAllDone   = "gauge:tasks_done"
	StoreHasTasks      = "turn:has_tasks"
	StoreTurnToolCalls  = "turn:tool_calls"
	StoreStepInputLen   = "turn:step_len"
	StoreStepInput      = "value:step"
	StoreExploreCalls   = "counter:explore_calls"
	StoreHasEdits       = "turn:has_edits"
	StoreRespGarbled    = "counter:garbled"
)
