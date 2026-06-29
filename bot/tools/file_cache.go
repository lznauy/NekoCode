package tools

import (
	"context"

	"nekocode/bot/tools/editcore"
	"nekocode/bot/tools/execution"
)

type ExecutionState = execution.ExecutionState

func NewExecutionState() *ExecutionState {
	return execution.NewExecutionState()
}

func WithExecutionState(ctx context.Context, state *ExecutionState) context.Context {
	return execution.WithExecutionState(ctx, state)
}

func FileCacheFromContext(ctx context.Context) *execution.FileStateCache {
	return execution.FileCacheFromContext(ctx)
}

// GetGlobalSnapshotStore returns the global snapshot store.
func GetGlobalSnapshotStore() *editcore.SnapshotStore { return execution.GetGlobalSnapshotStore() }
