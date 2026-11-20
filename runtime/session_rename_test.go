package runtime

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRenameSessionPublishesMetadataEventOnlyOnSuccess(t *testing.T) {
	failure := false
	calls := 0
	rt := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) { return "", nil }), ServiceGroups{Sessions: SessionsServices{
		CurrentSessionID: func() string { return "active" },
		ListSessions:     func() []SessionMeta { return nil },
		SessionMessages:  func() []DisplayMessage { return nil },
		RenameSession: func(id, name string) error {
			calls++
			if id != "other" || name != "custom name" {
				t.Fatalf("wrong rename %q %q", id, name)
			}
			if failure {
				return errors.New("write failed")
			}
			return nil
		},
	}})
	t.Cleanup(func() { _ = rt.Close() })
	if err := rt.RenameSession("other", "custom name"); err != nil {
		t.Fatal(err)
	}
	events := rt.events.History(EventFilter{Types: []EventType{EventSessionChanged}})
	if len(events) != 1 {
		t.Fatalf("events: %+v", events)
	}
	payload := events[0].Payload.(SessionPayload)
	if payload.Reason != "rename" || payload.ChangedID != "other" || payload.ID != "active" || payload.CurrentChanged {
		t.Fatalf("rename projected as switch: %+v", payload)
	}
	failure = true
	if err := rt.RenameSession("other", "custom name"); err == nil {
		t.Fatal("write failure hidden")
	}
	if events := rt.events.History(EventFilter{Types: []EventType{EventSessionChanged}}); len(events) != 1 {
		t.Fatal("failure published success")
	}
	rt.mu.Lock()
	rt.status = RunRunning
	rt.mu.Unlock()
	failure = false
	if err := rt.RenameSession("other", "custom name"); err != nil || calls != 3 {
		t.Fatalf("rename during run: %v, calls=%d", err, calls)
	}
	rt.mu.Lock()
	rt.status = RunIdle
	rt.mu.Unlock()
}

func TestRenameSessionObserverCanReenterRuntime(t *testing.T) {
	rt := New(RunnerFunc(func(context.Context, string, RunHost) (string, error) { return "", nil }), ServiceGroups{Sessions: SessionsServices{RenameSession: func(string, string) error { return nil }}})
	defer rt.Close()
	called := false
	rt.events.AddObserver(func(ev Event) {
		if ev.Type != EventSessionChanged {
			return
		}
		called = true
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		if _, err := rt.CancelRunWithResult(ctx, ""); err != nil {
			t.Errorf("observer could not reenter runtime: %v", err)
		}
	})
	if err := rt.RenameSession("s", "label"); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("session observer was not called")
	}
}
