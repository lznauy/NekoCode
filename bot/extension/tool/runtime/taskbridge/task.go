package taskbridge

import (
	"context"

	"nekocode/protocol"
)

// DefaultMaxConcurrentTasks is used when no delegated execution limit is configured.
const DefaultMaxConcurrentTasks = 8

// TaskStatus is the tool-layer view of delegated task completion.
type TaskStatus int

const (
	TaskStatusCompleted TaskStatus = iota
	TaskStatusFailed
	TaskStatusPartial
)

// TaskResult is returned by a delegated task runner.
type TaskResult struct {
	ID      string
	Status  TaskStatus
	Content string
}

// TaskSpec describes one delegated task without coupling the task tool to a
// concrete sub-agent implementation. Profile defines the capability ceiling;
// Skills supply task-scoped workflows and cannot grant additional tools.
type TaskSpec struct {
	ResumeID string
	Title    string
	Prompt   string
	Profile  string
	Skills   []string
}

// TaskRunner executes a delegated task without exposing the concrete sub-agent
// implementation to builtin tools.
type TaskRunner func(ctx context.Context, spec TaskSpec) (*TaskResult, error)

type TaskListFilter struct {
	Query  string
	Status string
	Offset int
	Limit  int
}

type TaskAttemptSummary struct {
	ID              string `json:"id"`
	Seq             int    `json:"seq"`
	Status          string `json:"status"`
	Prompt          string `json:"prompt"`
	Result          string `json:"result,omitempty"`
	TranscriptStart int    `json:"transcript_start"`
	TranscriptEnd   int    `json:"transcript_end"`
	TotalTokens     int    `json:"total_tokens,omitempty"`
	CacheHitTokens  int    `json:"cache_hit_tokens,omitempty"`
	CacheMissTokens int    `json:"cache_miss_tokens,omitempty"`
	ToolUseCount    int    `json:"tool_use_count,omitempty"`
	ContextTokens   int    `json:"context_tokens,omitempty"`
	ContextWindow   int    `json:"context_window,omitempty"`
}

type TaskSummary struct {
	Recorded      bool                `json:"recorded"`
	Title         string              `json:"title,omitempty"`
	ID            string              `json:"id"`
	Profile       string              `json:"profile"`
	Status        string              `json:"status"`
	Prompt        string              `json:"prompt"`
	Result        string              `json:"result,omitempty"`
	RunCount      int                 `json:"run_count"`
	LatestAttempt *TaskAttemptSummary `json:"latest_attempt,omitempty"`
}

type TaskPage struct {
	Items      []TaskSummary `json:"items"`
	SessionID  string        `json:"session_id"`
	Total      int           `json:"total"`
	NextOffset int           `json:"next_offset"`
	HasMore    bool          `json:"has_more"`
}

type TaskLister func(ctx context.Context, filter TaskListFilter) (TaskPage, error)

// TaskCallbackFn forwards delegated task tool events to the caller.
type TaskCallbackFn func(ev protocol.StepEvent)

// TaskCallback carries trusted runtime identity alongside delegated events.
type TaskCallback struct {
	ID       string
	Callback TaskCallbackFn
	Begin    func(context.Context) error
	// Finish is a fallback for calls rejected or cancelled before Execute runs.
	Finish func(protocol.StepEvent)
}

type taskCallbackCtxKey struct{}

// WithTaskCallback returns a child context carrying a delegated task callback.
func WithTaskCallback(ctx context.Context, cb TaskCallbackFn) context.Context {
	return context.WithValue(ctx, taskCallbackCtxKey{}, cb)
}

// TaskCallbackFromCtx retrieves a delegated task callback from context. A stored
// nil callback reports absence: callers invoke the result without a nil check,
// so a nil would otherwise panic at the delegation site.
func TaskCallbackFromCtx(ctx context.Context) (TaskCallbackFn, bool) {
	cb, ok := ctx.Value(taskCallbackCtxKey{}).(TaskCallbackFn)
	if !ok || cb == nil {
		return nil, false
	}
	return cb, true
}

// WithTaskID attaches the runtime's sub-agent identity to delegated interactions.
func WithTaskID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, taskIDKey{}, id)
}

type taskIDKey struct{}

func TaskIDFromCtx(ctx context.Context) string { id, _ := ctx.Value(taskIDKey{}).(string); return id }
