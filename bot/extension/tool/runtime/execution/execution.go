package execution

import (
	"context"

	"nekocode/bot/checkpoint"
	"nekocode/bot/extension/tool/runtime/core"
)

// ExecutionState carries mutable tool execution state that must be isolated
// per agent/sub-agent run.
type ExecutionState struct {
	OutputStore   *core.OutputStore
	FileCache     *FileStateCache
	SnapshotStore *SnapshotStore
	Checkpoints   *checkpoint.Manager
}

type executionStateCtxKey struct{}

func NewExecutionState() *ExecutionState {
	return &ExecutionState{
		OutputStore:   core.NewOutputStore(""),
		FileCache:     NewFileStateCache(),
		SnapshotStore: NewSnapshotStore(),
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

func SnapshotStoreFromContext(ctx context.Context) *SnapshotStore {
	if state := ExecutionStateFromContext(ctx); state != nil && state.SnapshotStore != nil {
		return state.SnapshotStore
	}
	return GetGlobalSnapshotStore()
}

var globalFileCache *FileStateCache
var globalSnapshotStore *SnapshotStore

// GetGlobalFileCache returns the global file state cache.
func GetGlobalFileCache() *FileStateCache { return globalFileCache }

// GetGlobalSnapshotStore returns the global snapshot store.
func GetGlobalSnapshotStore() *SnapshotStore { return globalSnapshotStore }

// RecordSnapshot stores a snapshot of path's current content in the global
// store and returns its content hash. Returns "" if the store is unavailable.
func RecordSnapshot(path, content string) string {
	return recordSnapshot(GetGlobalSnapshotStore(), path, content)
}

// RecordSnapshotInContext stores a snapshot using the context's store (or the
// global store if the context has none) and returns its content hash.
func RecordSnapshotInContext(ctx context.Context, path, content string) string {
	return recordSnapshot(SnapshotStoreFromContext(ctx), path, content)
}

func recordSnapshot(store *SnapshotStore, path, content string) string {
	if store == nil {
		return ""
	}
	return store.Record(path, content)
}
