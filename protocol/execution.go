package protocol

import "context"

// ExecutionIdentity correlates observations with their owning execution.
// It carries no permissions and must never be reconstructed from model input.
type ExecutionIdentity struct {
	RunID     string `json:"run_id,omitempty"`
	TaskID    string `json:"task_id,omitempty"`
	AttemptID string `json:"attempt_id,omitempty"`
}

type executionKey struct{}

// WithExecutionIdentity binds a value snapshot; children cannot modify a parent.
func WithExecutionIdentity(ctx context.Context, identity ExecutionIdentity) context.Context {
	return context.WithValue(ctx, executionKey{}, identity)
}

// ExecutionFromContext returns an empty identity outside an execution boundary.
func ExecutionFromContext(ctx context.Context) ExecutionIdentity {
	if ctx == nil {
		return ExecutionIdentity{}
	}
	identity, _ := ctx.Value(executionKey{}).(ExecutionIdentity)
	return identity
}
