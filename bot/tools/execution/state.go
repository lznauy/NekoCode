package execution

import (
	"context"

	"nekocode/bot/tools/editdsl"
)

// ExecutionState carries mutable tool execution state that must be isolated
// per agent/sub-agent run.
type ExecutionState struct {
	FileCache     *FileStateCache
	SnapshotStore *editdsl.SnapshotStore
}

type executionStateCtxKey struct{}

func NewExecutionState() *ExecutionState {
	return &ExecutionState{
		FileCache:     NewFileStateCache(),
		SnapshotStore: editdsl.NewSnapshotStore(),
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

func SnapshotStoreFromContext(ctx context.Context) *editdsl.SnapshotStore {
	if state := ExecutionStateFromContext(ctx); state != nil && state.SnapshotStore != nil {
		return state.SnapshotStore
	}
	return GetGlobalSnapshotStore()
}

var globalFileCache *FileStateCache
var globalSnapshotStore *editdsl.SnapshotStore

// SetGlobalFileCache sets the global file state cache.
func SetGlobalFileCache(c *FileStateCache) { globalFileCache = c }

// GetGlobalFileCache returns the global file state cache.
func GetGlobalFileCache() *FileStateCache { return globalFileCache }

// SetGlobalSnapshotStore sets the global snapshot store.
func SetGlobalSnapshotStore(s *editdsl.SnapshotStore) { globalSnapshotStore = s }

// GetGlobalSnapshotStore returns the global snapshot store.
func GetGlobalSnapshotStore() *editdsl.SnapshotStore { return globalSnapshotStore }
