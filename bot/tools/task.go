package tools

import "context"

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

// TaskRunner executes a delegated task without exposing the concrete sub-agent
// implementation to builtin tools.
type TaskRunner func(ctx context.Context, prompt, taskType, thoroughness string) (*TaskResult, error)

// TaskRunnerTool is implemented by tools that can execute delegated tasks.
type TaskRunnerTool interface {
	Wire(TaskRunner)
}

// TaskCallbackFn forwards delegated task tool events to the caller.
type TaskCallbackFn func(action, toolName, toolArgs, output string)

type taskCallbackCtxKey struct{}

// WithTaskCallback returns a child context carrying a delegated task callback.
func WithTaskCallback(ctx context.Context, cb TaskCallbackFn) context.Context {
	return context.WithValue(ctx, taskCallbackCtxKey{}, cb)
}

// TaskCallbackFromCtx retrieves a delegated task callback from context.
func TaskCallbackFromCtx(ctx context.Context) (TaskCallbackFn, bool) {
	cb, ok := ctx.Value(taskCallbackCtxKey{}).(TaskCallbackFn)
	return cb, ok
}
