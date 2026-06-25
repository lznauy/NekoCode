package execution

import (
	"context"

	"nekocode/bot/tools/editcore"
)

// ExecutionState carries mutable tool execution state that must be isolated
// per agent/sub-agent run.
type ExecutionState struct {
	FileCache     *FileStateCache
	SnapshotStore *editcore.SnapshotStore
	ViewStore     *ViewStore
}

type executionStateCtxKey struct{}

func NewExecutionState() *ExecutionState {
	return &ExecutionState{
		FileCache:     NewFileStateCache(),
		SnapshotStore: editcore.NewSnapshotStore(),
		ViewStore:     NewViewStore(),
	}
}

func WithExecutionState(ctx context.Context, state *ExecutionState) context.Context {
	if state == nil {
		return ctx
	}
	return context.WithValue(ctx, executionStateCtxKey{}, state)
}

func ExecutionStateFromContext(ctx context.Context) *ExecutionState {
	if ctx == nil {
		return nil
	}
	state, _ := ctx.Value(executionStateCtxKey{}).(*ExecutionState)
	return state
}

func FileCacheFromContext(ctx context.Context) *FileStateCache {
	if state := ExecutionStateFromContext(ctx); state != nil && state.FileCache != nil {
		return state.FileCache
	}
	return GetGlobalFileCache()
}

func SnapshotStoreFromContext(ctx context.Context) *editcore.SnapshotStore {
	if state := ExecutionStateFromContext(ctx); state != nil && state.SnapshotStore != nil {
		return state.SnapshotStore
	}
	return GetGlobalSnapshotStore()
}

func ViewStoreFromContext(ctx context.Context) *ViewStore {
	if state := ExecutionStateFromContext(ctx); state != nil && state.ViewStore != nil {
		return state.ViewStore
	}
	return GetGlobalViewStore()
}

var globalFileCache *FileStateCache
var globalSnapshotStore *editcore.SnapshotStore
var globalViewStore *ViewStore

// SetGlobalFileCache sets the global file state cache.
func SetGlobalFileCache(c *FileStateCache) { globalFileCache = c }

// GetGlobalFileCache returns the global file state cache.
func GetGlobalFileCache() *FileStateCache { return globalFileCache }

// SetGlobalSnapshotStore sets the global snapshot store.
func SetGlobalSnapshotStore(s *editcore.SnapshotStore) { globalSnapshotStore = s }

// GetGlobalSnapshotStore returns the global snapshot store.
func GetGlobalSnapshotStore() *editcore.SnapshotStore { return globalSnapshotStore }

// SetGlobalViewStore sets the global edit-aware read view store.
func SetGlobalViewStore(s *ViewStore) { globalViewStore = s }

// GetGlobalViewStore returns the global edit-aware read view store.
func GetGlobalViewStore() *ViewStore { return globalViewStore }
