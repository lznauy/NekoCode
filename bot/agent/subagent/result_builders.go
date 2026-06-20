package subagent

type runMeta struct {
	totalTokens     int
	toolUseCount    int
	durationMs      int64
	cacheHitTokens  int
	cacheMissTokens int
	sensitiveOps    int
}

func newResult(status Status, content string, meta runMeta, cls classification) *Result {
	return &Result{
		Status:          status,
		Content:         content,
		TotalTokens:     meta.totalTokens,
		ToolUseCount:    meta.toolUseCount,
		DurationMs:      meta.durationMs,
		CacheHitTokens:  meta.cacheHitTokens,
		CacheMissTokens: meta.cacheMissTokens,
		classification:  cls,
	}
}

func buildResult(rawOutput string, meta runMeta) *Result {
	return newResult(StatusCompleted, rawOutput, meta, classifyHandoff(rawOutput, meta))
}

func buildPartialResult(lastText string, meta runMeta) *Result {
	return newResult(StatusPartial, lastText, meta, classUnavailable)
}

func buildFailedResult(errMsg string, meta runMeta) *Result {
	return newResult(StatusFailed, errMsg, meta, classUnavailable)
}
