package tools

import (
	"context"

	"nekocode/bot/tools/editcore"
	"nekocode/bot/tools/execution"
)

type FileStateCache = execution.FileStateCache
type ExecutionState = execution.ExecutionState
type ViewStore = execution.ViewStore
type FileView = execution.FileView

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

func SnapshotStoreFromContext(ctx context.Context) *editcore.SnapshotStore {
	return execution.SnapshotStoreFromContext(ctx)
}

func ViewStoreFromContext(ctx context.Context) *ViewStore {
	return execution.ViewStoreFromContext(ctx)
}

// SetGlobalFileCache sets the global file state cache.
func SetGlobalFileCache(c *FileStateCache) { execution.SetGlobalFileCache(c) }

// GetGlobalFileCache returns the global file state cache.
func GetGlobalFileCache() *FileStateCache { return execution.GetGlobalFileCache() }

// SetGlobalSnapshotStore sets the global snapshot store.
func SetGlobalSnapshotStore(s *editcore.SnapshotStore) { execution.SetGlobalSnapshotStore(s) }

// GetGlobalSnapshotStore returns the global snapshot store.
func GetGlobalSnapshotStore() *editcore.SnapshotStore { return execution.GetGlobalSnapshotStore() }

// SetGlobalViewStore sets the global edit-aware read view store.
func SetGlobalViewStore(s *ViewStore) { execution.SetGlobalViewStore(s) }

// GetGlobalViewStore returns the global edit-aware read view store.
func GetGlobalViewStore() *ViewStore { return execution.GetGlobalViewStore() }

func NewFileStateCache() *FileStateCache {
	return execution.NewFileStateCache()
}

func NewViewStore() *ViewStore {
	return execution.NewViewStore()
}
