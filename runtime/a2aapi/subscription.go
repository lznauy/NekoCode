package a2aapi

import (
	"context"
	"iter"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/a2aproject/a2a-go/v2/a2asrv/taskstore"
)

// Task snapshots follow persisted updates from both the SDK and the background
// monitor. No SDK execution queue exists between INPUT_REQUIRED and a follow-up.
type taskSubscriptionHandler struct {
	a2asrv.RequestHandler
	store *retainedTaskStore
}

func (h *taskSubscriptionHandler) SubscribeToTask(ctx context.Context, req *a2a.SubscribeToTaskRequest) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		if req == nil || req.ID == "" {
			yield(nil, a2a.ErrInvalidParams)
			return
		}
		h.store.mu.RLock()
		entry := h.store.tasks[req.ID]
		h.store.mu.RUnlock()
		if entry == nil {
			yield(nil, a2a.ErrTaskNotFound)
			return
		}
		// Keep this entry until the terminal snapshot is delivered, even if a
		// newer task evicts it from the retained-store index in the meantime.
		var version taskstore.TaskVersion
		for {
			h.store.mu.RLock()
			changed := h.store.changed
			h.store.mu.RUnlock()
			stored, err := entry.store.Get(ctx, req.ID)
			if err != nil {
				yield(nil, err)
				return
			}
			if stored.Version != version {
				version = stored.Version
				if !yield(stored.Task, nil) {
					return
				}
			}
			if stored.Task.Status.State.Terminal() || stored.Task.Status.State == a2a.TaskStateInputRequired {
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-changed:
			}
		}
	}
}
