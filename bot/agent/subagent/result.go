package subagent

type Status int

const (
	StatusCompleted Status = iota
	StatusFailed
	StatusPartial
)

type classification int

const (
	classPass classification = iota
	classWarn
	classUnavailable
)

type Result struct {
	Status          Status
	Content         string
	TotalTokens     int
	ToolUseCount    int
	DurationMs      int64
	CacheHitTokens  int
	CacheMissTokens int
	classification  classification
}

type runMeta struct {
	totalTokens     int
	toolUseCount    int
	durationMs      int64
	cacheHitTokens  int
	cacheMissTokens int
	sensitiveOps    int
}

func FormatResult(r *Result) string {
	if r.classification == classWarn {
		return "SECURITY WARNING: This sub-agent performed actions that may violate security policy.\n\n" + r.Content
	}
	return r.Content
}

// classifyHandoff inspects both the subagent's text output and its actual tool
// operations (via meta.sensitiveOps) for dangerous patterns. This catches cases
// where a subagent performed sensitive operations (reading .env, running rm,
// etc.) but the text output doesn't mention the filenames or commands explicitly.
func classifyHandoff(rawOutput string, meta runMeta) classification {
	// Tool-call-based check: actual sensitive operations always trigger a warning,
	// even if the text output looks harmless.
	if meta.sensitiveOps > 0 {
		return classWarn
	}

	// Text-based check: catch dangerous patterns in the raw output.
	if isDangerousCommand(rawOutput) || isSensitivePath(rawOutput) {
		return classWarn
	}
	return classPass
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
