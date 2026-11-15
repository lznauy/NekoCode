package headless

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"nekocode/mode"
	"nekocode/protocol"
	rt "nekocode/runtime"
)

func TestBackgroundInteractionsAfterForegroundSettlement(t *testing.T) {
	release := make(chan struct{})
	done := make(chan struct{})
	root := t.TempDir()
	h := newHarnessWithServices(t, func(ctx context.Context, _ string, host rt.RunHost) (string, error) {
		if protocol.CoordinationFromContext(ctx).Notification {
			return "reviewed", nil
		}
		_, err := host.(protocol.BackgroundSubmitter).SubmitBackground(ctx, protocol.BackgroundWork{ID: "worker", SessionID: "session-test", ReadOnly: true, Run: func(ctx context.Context, host protocol.BackgroundHost) (protocol.BackgroundResult, error) {
			select {
			case <-release:
			case <-ctx.Done():
				return protocol.BackgroundResult{Text: ""}, ctx.Err()
			}
			reply := host.Confirm(protocol.ConfirmRequest{ToolName: "shell", CallID: "background-call", Args: map[string]any{"command": "pwd"}})
			if !reply.Allowed {
				return protocol.BackgroundResult{Text: "denied"}, nil
			}
			answer := host.Ask(protocol.QuestionRequest{Questions: []protocol.QuestionItem{{Header: "Choice", Question: "Continue?", Options: []protocol.QuestionOption{{Label: "yes"}}}}})
			if answer.Rejected {
				return protocol.BackgroundResult{Text: "question rejected"}, nil
			}
			close(done)
			return protocol.BackgroundResult{Text: "done"}, nil
		}})
		return "accepted", err
	}, Options{}, rt.ServiceGroups{Modes: rt.ModesServices{Current: func() mode.ID { return mode.SuperAgent }, Set: func(mode.ID) error { return nil }, Options: mode.Definitions, Resolve: func(string) (mode.Snapshot, error) { d, err := mode.Resolve(mode.SuperAgent); return d.Snapshot, err }}, Sessions: rt.SessionsServices{BackgroundStatePath: func(id string) string { return filepath.Join(root, id, "background.json") }}})
	h.send(t, user("initial", "start"))
	h.until(t, "result")
	if response := rpc(t, h, "session.new", nil); response["subtype"] != "error" {
		t.Fatal("session changed while background worker could still request an interaction", response)
	}
	close(release)
	request := h.until(t, "control_request")
	body := request["request"].(map[string]any)
	if body["nekocode_subagent_id"] != "worker" || body["tool_use_id"] != "background-call" {
		t.Fatal(request)
	}
	if response := rpc(t, h, "session.new", nil); response["subtype"] != "error" {
		t.Fatal("session changed with a pending background interaction", response)
	}
	h.send(t, map[string]any{"type": "control_response", "response": map[string]any{"subtype": "success", "request_id": request["request_id"], "response": map[string]any{"behavior": "allow"}}})
	request = h.until(t, "control_request")
	if request["request"].(map[string]any)["subtype"] != "nekocode_question" {
		t.Fatal(request)
	}
	h.send(t, map[string]any{"type": "control_response", "response": map[string]any{"subtype": "success", "request_id": request["request_id"], "response": map[string]any{"answers": [][]string{{"yes"}}}}})
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("background interaction did not settle")
	}
	h.finish(t)
}

func TestAutomaticCoordinatorInteractionsAndResult(t *testing.T) {
	release := make(chan struct{})
	root := t.TempDir()
	h := newHarnessWithServices(t, func(ctx context.Context, _ string, host rt.RunHost) (string, error) {
		if protocol.CoordinationFromContext(ctx).Notification {
			if !host.Confirm(protocol.ConfirmRequest{ToolName: "shell", CallID: "coordinator-call", Args: map[string]any{"command": "pwd"}}).Allowed {
				return "denied", nil
			}
			answer := host.Ask(protocol.QuestionRequest{Questions: []protocol.QuestionItem{{Header: "Choice", Question: "Continue?", Options: []protocol.QuestionOption{{Label: "yes"}}}}})
			if answer.Rejected {
				return "question rejected", nil
			}
			return "coordinator completed", nil
		}
		_, err := host.(protocol.BackgroundSubmitter).SubmitBackground(ctx, protocol.BackgroundWork{ID: "worker", GroupID: "plan", SessionID: "session-test", ReadOnly: true, Run: func(ctx context.Context, _ protocol.BackgroundHost) (protocol.BackgroundResult, error) {
			select {
			case <-release:
				return protocol.BackgroundResult{Text: "worker completed"}, nil
			case <-ctx.Done():
				return protocol.BackgroundResult{Text: ""}, ctx.Err()
			}
		}})
		return "accepted", err
	}, Options{}, rt.ServiceGroups{Modes: rt.ModesServices{Current: func() mode.ID { return mode.SuperAgent }, Set: func(mode.ID) error { return nil }, Options: mode.Definitions, Resolve: func(string) (mode.Snapshot, error) { d, err := mode.Resolve(mode.SuperAgent); return d.Snapshot, err }}, Sessions: rt.SessionsServices{BackgroundStatePath: func(id string) string { return filepath.Join(root, id, "background.json") }}})
	h.send(t, user("initial", "start"))
	foreground := h.until(t, "result")
	close(release)
	approval := h.until(t, "control_request")
	body := approval["request"].(map[string]any)
	if body["subtype"] != "can_use_tool" || body["tool_use_id"] != "coordinator-call" {
		t.Fatal(approval)
	}
	if response := rpc(t, h, "session.new", nil); response["subtype"] != "error" {
		t.Fatal("session changed during automatic coordinator approval", response)
	}
	h.send(t, reply(approval["request_id"], map[string]any{"behavior": "allow"}))
	question := h.until(t, "control_request")
	if question["request"].(map[string]any)["subtype"] != "nekocode_question" {
		t.Fatal(question)
	}
	h.send(t, reply(question["request_id"], map[string]any{"answers": [][]string{{"yes"}}}))
	result := h.until(t, "result")
	if result["result"] != "coordinator completed" || result["run_id"] == foreground["run_id"] {
		t.Fatal(result)
	}
	h.finish(t)
}

type coordinatorOwnershipBackend struct {
	Backend
	run     rt.RunSnapshot
	tasks   []rt.BackgroundTask
	current string
}

func TestAutomaticCoordinatorInterruptCancelsPendingApproval(t *testing.T) {
	release := make(chan struct{})
	root := t.TempDir()
	h := newHarnessWithServices(t, func(ctx context.Context, _ string, host rt.RunHost) (string, error) {
		if protocol.CoordinationFromContext(ctx).Notification {
			host.Confirm(protocol.ConfirmRequest{ToolName: "shell", CallID: "coordinator-call"})
			return "", ctx.Err()
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
	}, Options{}, rt.ServiceGroups{Modes: rt.ModesServices{Current: func() mode.ID { return mode.SuperAgent }, Set: func(mode.ID) error { return nil }, Options: mode.Definitions, Resolve: func(string) (mode.Snapshot, error) { d, err := mode.Resolve(mode.SuperAgent); return d.Snapshot, err }}, Sessions: rt.SessionsServices{BackgroundStatePath: func(id string) string { return filepath.Join(root, id, "background.json") }}})
	h.send(t, user("initial", "start"))
	h.until(t, "result")
	close(release)
	request := h.until(t, "control_request")
	h.send(t, control("stop-coordinator", "interrupt"))
	if result := h.until(t, "result"); result["stop_reason"] != "cancelled" || result["is_error"] != true {
		t.Fatal(result)
	}
	// A late reply to the cancelled interaction must not revive the run.
	h.send(t, reply(request["request_id"], map[string]any{"behavior": "allow"}))
	h.finish(t)
}

func (b coordinatorOwnershipBackend) LookupRun(id rt.RunID) (rt.RunSnapshot, bool) {
	return b.run, id == b.run.ID
}
func (b coordinatorOwnershipBackend) BackgroundTasks(string) ([]rt.BackgroundTask, error) {
	return b.tasks, nil
}
func (b coordinatorOwnershipBackend) CurrentSessionID() string { return b.current }

func TestCoordinatorBindingRequiresRecordedRunAndSessionPlan(t *testing.T) {
	for _, scenario := range []string{"valid", "unrelated-source", "unknown-run", "wrong-plan", "wrong-session", "record-mismatch"} {
		t.Run(scenario, func(t *testing.T) {
			event := rt.Event{RunID: "review", Type: rt.EventRunStarted, Source: rt.SourceRef{Kind: "task_completion", ID: "plan"}}
			backend := coordinatorOwnershipBackend{current: "session", run: rt.RunSnapshot{ID: event.RunID, Source: event.Source, SessionID: "session"}, tasks: []rt.BackgroundTask{{ID: "worker", SessionID: "session", GroupID: "plan"}}}
			switch scenario {
			case "unrelated-source":
				event.Source.Kind = "gui"
			case "unknown-run":
				event.RunID = "other"
			case "wrong-plan":
				backend.tasks[0].GroupID = "other"
			case "wrong-session":
				backend.current = "other"
			case "record-mismatch":
				backend.run.Source.ID = "other"
			}
			s := &server{backend: backend, sessionID: "session"}
			if s.ownsCoordinatorRun(event) != (scenario == "valid") {
				t.Fatalf("unexpected coordinator ownership for %s", scenario)
			}
		})
	}
}

func TestForegroundSettlementDoesNotRejectBackgroundApproval(t *testing.T) {
	release, foregroundStarted, foregroundDone := make(chan struct{}), make(chan struct{}), make(chan struct{})
	decision := make(chan bool, 1)
	root := t.TempDir()
	h := newHarnessWithServices(t, func(ctx context.Context, input string, host rt.RunHost) (string, error) {
		if protocol.CoordinationFromContext(ctx).Notification {
			return "reviewed", nil
		}
		if input == "foreground" {
			close(foregroundStarted)
			select {
			case <-foregroundDone:
			case <-ctx.Done():
				return "", ctx.Err()
			}
			return "foreground done", nil
		}
		_, err := host.(protocol.BackgroundSubmitter).SubmitBackground(ctx, protocol.BackgroundWork{ID: "worker", SessionID: "session-test", ReadOnly: true, Run: func(ctx context.Context, host protocol.BackgroundHost) (protocol.BackgroundResult, error) {
			select {
			case <-release:
			case <-ctx.Done():
				return protocol.BackgroundResult{Text: ""}, ctx.Err()
			}
			reply := host.Confirm(protocol.ConfirmRequest{ToolName: "shell", CallID: "background-call", Args: map[string]any{"command": "pwd"}})
			decision <- reply.Allowed
			return protocol.BackgroundResult{Text: "done"}, nil
		}})
		return "accepted", err
	}, Options{}, rt.ServiceGroups{Modes: rt.ModesServices{Current: func() mode.ID { return mode.SuperAgent }, Set: func(mode.ID) error { return nil }, Options: mode.Definitions, Resolve: func(string) (mode.Snapshot, error) { d, err := mode.Resolve(mode.SuperAgent); return d.Snapshot, err }}, Sessions: rt.SessionsServices{BackgroundStatePath: func(id string) string { return filepath.Join(root, id, "background.json") }}})
	h.send(t, user("initial", "start"))
	h.until(t, "result")
	h.send(t, user("next", "foreground"))
	select {
	case <-foregroundStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("foreground not started")
	}
	close(release)
	request := h.until(t, "control_request")
	close(foregroundDone)
	h.until(t, "result")
	h.send(t, reply(request["request_id"], map[string]any{"behavior": "allow"}))
	select {
	case allowed := <-decision:
		if !allowed {
			t.Fatal("foreground settlement rejected the background approval")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("approval not resolved")
	}
	h.finish(t)
}

func TestForegroundInterruptDoesNotRejectBackgroundApproval(t *testing.T) {
	release, foregroundStarted, foregroundDone := make(chan struct{}), make(chan struct{}), make(chan struct{})
	decision := make(chan bool, 1)
	root := t.TempDir()
	h := newHarnessWithServices(t, func(ctx context.Context, input string, host rt.RunHost) (string, error) {
		if protocol.CoordinationFromContext(ctx).Notification {
			return "reviewed", nil
		}
		if input == "foreground" {
			close(foregroundStarted)
			select {
			case <-foregroundDone:
			case <-ctx.Done():
				return "", ctx.Err()
			}
			return "foreground done", nil
		}
		_, err := host.(protocol.BackgroundSubmitter).SubmitBackground(ctx, protocol.BackgroundWork{ID: "worker", SessionID: "session-test", ReadOnly: true, Run: func(ctx context.Context, host protocol.BackgroundHost) (protocol.BackgroundResult, error) {
			select {
			case <-release:
			case <-ctx.Done():
				return protocol.BackgroundResult{Text: ""}, ctx.Err()
			}
			reply := host.Confirm(protocol.ConfirmRequest{ToolName: "shell", CallID: "background-call", Args: map[string]any{"command": "pwd"}})
			decision <- reply.Allowed
			return protocol.BackgroundResult{Text: "done"}, nil
		}})
		return "accepted", err
	}, Options{}, rt.ServiceGroups{Modes: rt.ModesServices{Current: func() mode.ID { return mode.SuperAgent }, Set: func(mode.ID) error { return nil }, Options: mode.Definitions, Resolve: func(string) (mode.Snapshot, error) { d, err := mode.Resolve(mode.SuperAgent); return d.Snapshot, err }}, Sessions: rt.SessionsServices{BackgroundStatePath: func(id string) string { return filepath.Join(root, id, "background.json") }}})
	h.send(t, user("initial", "start"))
	h.until(t, "result")
	h.send(t, user("next", "foreground"))
	select {
	case <-foregroundStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("foreground not started")
	}
	close(release)
	request := h.until(t, "control_request")
	h.send(t, control("interrupt-foreground", "interrupt"))
	h.until(t, "result")
	h.send(t, reply(request["request_id"], map[string]any{"behavior": "allow"}))
	select {
	case allowed := <-decision:
		if !allowed {
			t.Fatal("foreground settlement rejected the background approval")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("approval not resolved")
	}
	h.finish(t)
}
