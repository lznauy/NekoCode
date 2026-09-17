package taskbridge

import (
	"context"

	"nekocode/protocol"
)

// TaskStatus is the tool-layer view of delegated task completion.
type TaskStatus int

const (
	TaskStatusCompleted TaskStatus = iota
	TaskStatusFailed
	TaskStatusPartial
)

// TaskResult is returned by a delegated task runner.
type TaskResult struct {
	Status  TaskStatus
	Content string
}

// TaskSpec describes one delegated task without coupling the task tool to a
// concrete sub-agent implementation. Profile defines the capability ceiling;
// Skills supply task-scoped workflows and cannot grant additional tools.
type TaskSpec struct {
	Prompt  string
	Profile string
	Skills  []string
}

// TaskRunner executes a delegated task without exposing the concrete sub-agent
// implementation to builtin tools.
type TaskRunner func(ctx context.Context, spec TaskSpec) (*TaskResult, error)

// TaskCallbackFn forwards delegated task tool events to the caller.
type TaskCallbackFn func(ev protocol.StepEvent)

// TaskCallback carries trusted runtime identity alongside delegated events.
type TaskCallback struct {
	ID       string
	Callback TaskCallbackFn
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
