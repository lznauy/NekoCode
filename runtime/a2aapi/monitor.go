package a2aapi

import (
	"context"
	"errors"
	"log/slog"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/a2aproject/a2a-go/v2/a2asrv/taskstore"
)

// Cleanup runs after the SDK has persisted INPUT_REQUIRED. A separate reader
// follows the runtime while no SDK execution exists, including resolutions
// submitted through the daemon's other transports.
func (e *executor) Cleanup(ctx context.Context, execCtx *a2asrv.ExecutorContext, result a2a.SendMessageResult, err error) {
	binding := e.lookup(execCtx.TaskID)
	if execCtx.Message != nil {
		if binding != nil {
			e.releaseForeground(ctx, binding)
		}
	}
	// The SDK can fail to persist INPUT_REQUIRED after the producer has
	// already returned. That leaves no producer to observe cancellation.
	if execCtx.Message != nil && binding != nil {
		task, _ := result.(*a2a.Task)
		if err != nil || (task != nil && task.Status.State.Terminal()) {
			e.failBinding(context.WithoutCancel(ctx), execCtx, binding)
			return
		}
	}
	if e.store == nil || err != nil {
		return
	}
	task, ok := result.(*a2a.Task)
	if !ok || task.Status.State.Terminal() {
		return
	}
	e.startMonitor(ctx, execCtx)
}

func (e *executor) startMonitor(ctx context.Context, execCtx *a2asrv.ExecutorContext) {
	binding := e.lookup(execCtx.TaskID)
	if binding == nil {
		return
	}
	binding.mu.Lock()
	if binding.terminal || binding.monitorCancel != nil || binding.foreground > 0 || binding.monitorPauses > 0 {
		binding.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	done := make(chan struct{})
	binding.monitorCancel, binding.monitorDone = cancel, done
	binding.mu.Unlock()
	go func() {
		defer close(done)
		defer cancel()
		e.streamRunMode(ctx, execCtx, binding, func(event a2a.Event, eventErr error) bool {
			if eventErr != nil {
				return false
			}
			// Once read, persist the event even if a follow-up requests handoff.
			if err := e.persistEvent(context.WithoutCancel(ctx), execCtx.TaskID, event); err != nil {
				slog.Error("persist A2A background event", "task", execCtx.TaskID, "error", err)
				e.failBinding(context.WithoutCancel(ctx), execCtx, binding)
				return false
			}
			return true
		}, true)
	}()
}

// failBinding leaves a small, evictable terminal task and stops the runtime.
// Dropping retained payload here is necessary when that payload is the reason
// the ordinary task update could not fit the store's byte budget.
func (e *executor) failBinding(ctx context.Context, execCtx *a2asrv.ExecutorContext, binding *taskBinding) {
	defer e.abandonRun(execCtx.TaskID, binding)
	if e.store == nil {
		return
	}
	for {
		stored, err := e.store.Get(ctx, execCtx.TaskID)
		if err != nil || stored.Task.Status.State.Terminal() {
			return
		}
		failed := &a2a.Task{ID: stored.Task.ID, ContextID: stored.Task.ContextID, Status: a2a.TaskStatus{State: a2a.TaskStateFailed}}
		_, err = e.store.Update(ctx, &taskstore.UpdateRequest{Task: failed, PrevTask: stored.Task, PrevVersion: stored.Version})
		if errors.Is(err, taskstore.ErrConcurrentModification) {
			continue
		}
		if err != nil {
			slog.Error("finalize failed A2A task", "task", execCtx.TaskID, "error", err)
		}
		return
	}
}

func (e *executor) stopMonitor(binding *taskBinding) {
	binding.mu.Lock()
	cancel, done := binding.monitorCancel, binding.monitorDone
	binding.mu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	<-done
	binding.mu.Lock()
	if binding.monitorDone == done {
		binding.monitorCancel, binding.monitorDone = nil, nil
	}
	binding.mu.Unlock()
}

func (e *executor) persistEvent(ctx context.Context, taskID a2a.TaskID, event a2a.Event) error {
	for {
		stored, err := e.store.Get(ctx, taskID)
		if err != nil {
			return err
		}
		if stored.Task.Status.State.Terminal() {
			return nil
		}
		previous := *stored.Task
		switch event := event.(type) {
		case *a2a.TaskStatusUpdateEvent:
			stored.Task.Status = event.Status
			if event.Status.Message != nil {
				stored.Task.History = append(stored.Task.History, event.Status.Message)
			}
		case *a2a.TaskArtifactUpdateEvent:
			found := false
			for i, artifact := range stored.Task.Artifacts {
				if artifact.ID != event.Artifact.ID {
					continue
				}
				found = true
				if event.Append {
					artifact.Parts = append(artifact.Parts, event.Artifact.Parts...)
				} else {
					stored.Task.Artifacts[i] = event.Artifact
				}
				break
			}
			if !found {
				stored.Task.Artifacts = append(stored.Task.Artifacts, event.Artifact)
			}
		default:
			return nil
		}
		_, err = e.store.Update(ctx, &taskstore.UpdateRequest{
			Task: stored.Task, Event: event, PrevTask: &previous, PrevVersion: stored.Version,
		})
		if errors.Is(err, taskstore.ErrConcurrentModification) {
			continue
		}
		return err
	}
}

var _ a2asrv.AgentExecutionCleaner = (*executor)(nil)

func (e *executor) releaseForeground(ctx context.Context, binding *taskBinding) {
	binding.mu.Lock()
	defer binding.mu.Unlock()
	if handoff, _ := ctx.Value(monitorHandoffKey{}).(*monitorHandoff); handoff != nil {
		if !handoff.owned {
			return
		}
		handoff.owned = false
	}
	if binding.foreground > 0 {
		binding.foreground--
	}
}
