package tools

import (
	"context"

	"nekocode/bot/tools/editdsl"
	"nekocode/bot/tools/execution"
)

type FileStateCache = execution.FileStateCache
type ExecutionState = execution.ExecutionState

func NewExecutionState() *ExecutionState {
	return execution.NewExecutionState()
}

func WithExecutionState(ctx context.Context, state *ExecutionState) context.Context {
	return execution.WithExecutionState(ctx, state)
}

func ExecutionStateFromContext(ctx context.Context) *ExecutionState {
	return execution.ExecutionStateFromContext(ctx)
}

func FileCacheFromContext(ctx context.Context) *FileStateCache {
	return execution.FileCacheFromContext(ctx)
}

func SnapshotStoreFromContext(ctx context.Context) *editdsl.SnapshotStore {
	return execution.SnapshotStoreFromContext(ctx)
}

// SetGlobalFileCache sets the global file state cache.
func SetGlobalFileCache(c *FileStateCache) { execution.SetGlobalFileCache(c) }

// GetGlobalFileCache returns the global file state cache.
func GetGlobalFileCache() *FileStateCache { return execution.GetGlobalFileCache() }

// SetGlobalSnapshotStore sets the global snapshot store.
func SetGlobalSnapshotStore(s *editdsl.SnapshotStore) { execution.SetGlobalSnapshotStore(s) }

// GetGlobalSnapshotStore returns the global snapshot store.
func GetGlobalSnapshotStore() *editdsl.SnapshotStore { return execution.GetGlobalSnapshotStore() }

func NewFileStateCache() *FileStateCache {
	return execution.NewFileStateCache()
}
