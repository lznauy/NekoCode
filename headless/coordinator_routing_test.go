package headless

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"nekocode/mode"
	"nekocode/protocol"
	rt "nekocode/runtime"
)

// Delay delivery, never ordering: runtime can execute before the adapter has
// consumed its RunStarted event. Input and event streams advance independently.
type delayedCoordinatorBackend struct {
	*rt.Runtime
	held     chan rt.Event
	advance  chan struct{}
	queued   chan struct{}
	delayAll bool
}

func (b *delayedCoordinatorBackend) Events(ctx context.Context, filter rt.EventFilter) (<-chan rt.Event, error) {
	input, err := b.Runtime.Events(ctx, filter)
	if err != nil {
		return nil, err
	}
	output := make(chan rt.Event, 256)
	go func() {
		defer close(output)
		for event := range input {
			if event.Type == rt.EventRunStarted && (event.Source.Kind == "task_completion" || b.delayAll) {
				select {
				case b.held <- event:
				case <-ctx.Done():
					return
				}
				select {
				case <-b.advance:
				case <-ctx.Done():
					return
				}
			}
			select {
			case output <- event:
			case <-ctx.Done():
				return
			}
		}
	}()
	return output, nil
}
func (b *delayedCoordinatorBackend) StartRun(ctx context.Context, input rt.Input) (rt.RunID, error) {
	id, err := b.Runtime.StartRun(ctx, input)
	if input.Text == "next" {
		close(b.queued)
	}
	return id, err
}
func coordinatorServices(root string) rt.ServiceGroups {
	return rt.ServiceGroups{Modes: rt.ModesServices{Current: func() mode.ID { return mode.SuperAgent }, Set: func(mode.ID) error { return nil }, Options: mode.Definitions, Resolve: func(string) (mode.Snapshot, error) { d, err := mode.Resolve(mode.SuperAgent); return d.Snapshot, err }}, Sessions: rt.SessionsServices{BackgroundStatePath: func(id string) string { return filepath.Join(root, id, "background.json") }}}
}
func TestQueuedInputDoesNotHideAutomaticCoordinator(t *testing.T) {
	release := make(chan struct{})
	backend := &delayedCoordinatorBackend{held: make(chan rt.Event, 1), advance: make(chan struct{}), queued: make(chan struct{})}
	h := newHarnessWithBackend(t, func(ctx context.Context, input string, host rt.RunHost) (string, error) {
		if protocol.CoordinationFromContext(ctx).Notification {
			host.Confirm(protocol.ConfirmRequest{ToolName: "shell", CallID: "review-approval"})
			return "reviewed", ctx.Err()
		}
		if input == "next" {
			return "next complete", nil
		}
		_, err := host.(protocol.BackgroundSubmitter).SubmitBackground(ctx, protocol.BackgroundWork{ID: "worker", GroupID: "plan", SessionID: "session-test", ReadOnly: true, Run: func(ctx context.Context, _ protocol.BackgroundHost) (protocol.BackgroundResult, error) {
			select {
			case <-release:
				return protocol.BackgroundResult{Text: "done"}, nil
			case <-ctx.Done():
				return protocol.BackgroundResult{Text: ""}, ctx.Err()
			}
		}})
		return "accepted", err
	}, Options{}, coordinatorServices(t.TempDir()), func(runtime *rt.Runtime) Backend { backend.Runtime = runtime; return backend })
	h.send(t, user("first", "first"))
	h.until(t, "result")
	close(release)
	select {
	case <-backend.held:
	case <-time.After(time.Second):
		t.Fatal("coordinator not started")
	}
	h.send(t, user("second", "next"))
	select {
	case <-backend.queued:
	case <-time.After(time.Second):
		t.Fatal("next input not admitted")
	}
	close(backend.advance)
	request := h.until(t, "control_request")
	if request["request"].(map[string]any)["tool_use_id"] != "review-approval" {
		t.Fatal(request)
	}
	h.send(t, reply(request["request_id"], map[string]any{"behavior": "allow"}))
	if result := h.until(t, "result"); result["result"] != "reviewed" {
		t.Fatal(result)
	}
	if result := h.until(t, "result"); result["result"] != "next complete" || result["user_message_uuid"] != "second" {
		t.Fatal(result)
	}
	h.finish(t)
}
func TestSessionSwitchCannotBindOldCoordinator(t *testing.T) {
	root := t.TempDir()
	var sessionMu sync.Mutex
	current := ""
	sessionNum := 0
	path := filepath.Join(root, "B", "background.json")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"version":2,"tasks":[{"id":"B-worker","session_id":"B","group_id":"plan","status":"completed","notified":true}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	services := coordinatorServices(root)
	services.Sessions.NewSession = func() (rt.SessionMeta, error) {
		sessionMu.Lock()
		defer sessionMu.Unlock()
		sessionNum++
		if sessionNum == 1 {
			current = "A"
		} else {
			current = "B"
		}
		return rt.SessionMeta{ID: current}, nil
	}
	services.Sessions.CurrentSessionID = func() string { sessionMu.Lock(); defer sessionMu.Unlock(); return current }
	release := make(chan struct{})
	backend := &delayedCoordinatorBackend{held: make(chan rt.Event, 1), advance: make(chan struct{})}
	h := newHarnessWithBackend(t, func(ctx context.Context, input string, host rt.RunHost) (string, error) {
		if protocol.CoordinationFromContext(ctx).Notification {
			return "PRIVATE SESSION A REVIEW", nil
		}
		_, err := host.(protocol.BackgroundSubmitter).SubmitBackground(ctx, protocol.BackgroundWork{ID: "A-worker", GroupID: "plan", SessionID: "A", ReadOnly: true, Run: func(ctx context.Context, _ protocol.BackgroundHost) (protocol.BackgroundResult, error) {
			select {
			case <-release:
				return protocol.BackgroundResult{Text: "done"}, nil
			case <-ctx.Done():
				return protocol.BackgroundResult{Text: ""}, ctx.Err()
			}
		}})
		return "accepted", err
	}, Options{}, services, func(runtime *rt.Runtime) Backend { backend.Runtime = runtime; return backend })
	h.send(t, user("first", "first"))
	h.until(t, "result")
	close(release)
	var event rt.Event
	select {
	case event = <-backend.held:
	case <-time.After(time.Second):
		t.Fatal("coordinator not started")
	}
	if err := backend.WaitRun(t.Context(), event.RunID); err != nil {
		t.Fatal(err)
	}
	if response := rpc(t, h, "session.new", nil); response["subtype"] == "error" {
		t.Fatal(response)
	}
	close(backend.advance)
	// An ordered read-only RPC proves all prior delivered run events were handled.
	h.send(t, control("barrier", "server.info"))
	deadline := time.After(time.Second)
	for {
		select {
		case frame := <-h.frames:
			if frame["type"] == "result" && frame["result"] == "PRIVATE SESSION A REVIEW" {
				t.Fatalf("old session result rebound to B: %+v", frame)
			}
		case <-deadline:
			h.finish(t)
			return
		}
	}
}
func TestReviewDefaultInterruptBeforeStartEvent(t *testing.T) {
	backend := &delayedCoordinatorBackend{held: make(chan rt.Event, 1), advance: make(chan struct{}), delayAll: true}
	h := newHarnessWithBackend(t, func(ctx context.Context, _ string, _ rt.RunHost) (string, error) { <-ctx.Done(); return "", ctx.Err() }, Options{}, rt.ServiceGroups{}, func(runtime *rt.Runtime) Backend { backend.Runtime = runtime; return backend })
	h.send(t, user("first", "first"))
	var started rt.Event
	select {
	case started = <-backend.held:
	case <-time.After(time.Second):
		t.Fatal("runtime did not start")
	}
	response := rpc(t, h, "interrupt", nil)
	if response["subtype"] == "error" {
		t.Fatal(response)
	}
	if err := backend.WaitRun(t.Context(), started.RunID); err != nil {
		t.Fatal(err)
	}
	if run, ok := backend.LookupRun(started.RunID); !ok || run.Status != rt.RunCancelled {
		t.Fatalf("successful interrupt did not cancel the started run: %+v %v", run, ok)
	}
	close(backend.advance)
	if result := h.until(t, "result"); result["stop_reason"] != "cancelled" {
		t.Fatal(result)
	}
	h.finish(t)
}
func TestReviewCancelQueuedBeforeStartEventKeepsResult(t *testing.T) {
	backend := &delayedCoordinatorBackend{held: make(chan rt.Event, 1), advance: make(chan struct{}), delayAll: true}
	h := newHarnessWithBackend(t, func(ctx context.Context, _ string, _ rt.RunHost) (string, error) { <-ctx.Done(); return "", ctx.Err() }, Options{}, rt.ServiceGroups{}, func(runtime *rt.Runtime) Backend { backend.Runtime = runtime; return backend })
	h.send(t, user("first", "first"))
	select {
	case <-backend.held:
	case <-time.After(time.Second):
		t.Fatal("runtime did not start")
	}
	response := rpc(t, h, "interrupt", map[string]any{"cancel_queued": true})
	if response["subtype"] == "error" {
		t.Fatal(response)
	}
	close(backend.advance)
	result := h.until(t, "result")
	if result["stop_reason"] != "cancelled" {
		t.Fatal(result)
	}
	h.finish(t)
}

func TestShutdownCancelBeforeStartEventKeepsTerminalResult(t *testing.T) {
	backend := &delayedCoordinatorBackend{held: make(chan rt.Event, 1), advance: make(chan struct{}), delayAll: true}
	h := newHarnessWithBackend(t, func(ctx context.Context, _ string, _ rt.RunHost) (string, error) { <-ctx.Done(); return "", ctx.Err() }, Options{}, rt.ServiceGroups{}, func(runtime *rt.Runtime) Backend { backend.Runtime = runtime; return backend })
	h.send(t, user("first", "first"))
	select {
	case <-backend.held:
	case <-time.After(time.Second):
		t.Fatal("runtime did not start")
	}
	if response := rpc(t, h, "shutdown", map[string]any{"mode": "cancel"}); response["subtype"] == "error" {
		t.Fatal(response)
	}
	close(backend.advance)
	if result := h.until(t, "result"); result["stop_reason"] != "cancelled" {
		t.Fatal(result)
	}
	h.finish(t)
}

func TestEOFDrainsAcceptedTurnBeforeStartEvent(t *testing.T) {
	release := make(chan struct{})
	backend := &delayedCoordinatorBackend{held: make(chan rt.Event, 1), advance: make(chan struct{}), delayAll: true}
	h := newHarnessWithBackend(t, func(ctx context.Context, _ string, _ rt.RunHost) (string, error) {
		select {
		case <-release:
			return "done", nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}, Options{}, rt.ServiceGroups{}, func(runtime *rt.Runtime) Backend { backend.Runtime = runtime; return backend })
	h.send(t, user("first", "first"))
	select {
	case <-backend.held:
	case <-time.After(time.Second):
		t.Fatal("runtime did not start")
	}
	_ = h.in.Close()
	close(release)
	close(backend.advance)
	if result := h.until(t, "result"); result["result"] != "done" {
		t.Fatal(result)
	}
	h.finish(t)
}
