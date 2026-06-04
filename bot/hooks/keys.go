package hooks

// Store keys used by builtin hooks and the agent loop.
const (
	StoreToolPrefix     = "counter:tool:" // + name
	StoreToolResearcher = "turn:researcher"
	StoreFileModified   = "flag:modified"
	StoreQuotaReads     = "gauge:quota_reads"
	StoreExploreScore   = "gauge:explore"
	StoreTasksAllDone   = "gauge:tasks_done"
	StoreStepInput      = "value:step"
	StoreRespGarbled    = "counter:garbled"
	StoreRespChat       = "turn:chat"
)
