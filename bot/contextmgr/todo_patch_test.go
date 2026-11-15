package contextmgr

import (
	"errors"
	"nekocode/protocol"
	"testing"
)

func TestTodoPatchPersistenceFailureDoesNotLoseOtherSteps(t *testing.T) {
	m := New(Config{})
	plan, err := m.ApplyTodos(protocol.TodoUpdate{Items: []protocol.TodoItem{{Content: "a", Status: "pending"}, {Content: "b", Status: "pending"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	update := protocol.TodoUpdate{Patch: true, Items: []protocol.TodoItem{{ID: plan.Items[0].ID, Status: "completed", Result: "verified"}}}
	failure := errors.New("disk full")
	if _, err = m.ApplyTodos(update, func() error { return failure }); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	restored, err := m.ApplyTodos(protocol.TodoUpdate{PlanID: plan.ID, SelectOnly: true}, nil)
	if err != nil || len(restored.Items) != 2 || restored.Items[0].Status != "pending" {
		t.Fatalf("failed patch leaked: %+v, %v", restored, err)
	}
	updated, err := m.ApplyTodos(update, nil)
	if err != nil || len(updated.Items) != 2 || updated.Items[1].ID != plan.Items[1].ID {
		t.Fatalf("patch retry lost work: %+v, %v", updated, err)
	}
}

func TestTodoPatchMigratesRestoredLegacyItemsOnce(t *testing.T) {
	m := New(Config{})
	snapshot := m.Snapshot()
	snapshot.Todos = []protocol.TodoItem{{Content: "legacy work", Status: "pending"}}
	m.Restore(snapshot)
	plan, err := m.ApplyTodos(protocol.TodoUpdate{Patch: true, Items: []protocol.TodoItem{{Content: "new work", Status: "pending"}}}, nil)
	if err != nil || len(plan.Items) != 2 || plan.Items[0].ID == "" {
		t.Fatalf("legacy migration failed: %+v, %v", plan, err)
	}
	firstID := plan.Items[0].ID
	plan, err = m.ApplyTodos(protocol.TodoUpdate{Patch: true, Items: []protocol.TodoItem{{ID: firstID, Status: "completed", Result: "verified"}}}, nil)
	if err != nil || plan.Items[0].ID != firstID {
		t.Fatalf("legacy ID changed: %+v, %v", plan, err)
	}
}

func TestTodoUpdatesRejectUnknownIDsWithoutChangingPlan(t *testing.T) {
	for _, patch := range []bool{false, true} {
		m := New(Config{})
		plan, err := m.ApplyTodos(protocol.TodoUpdate{Items: []protocol.TodoItem{{Content: "inspect", Status: "pending"}}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		persisted := false
		_, err = m.ApplyTodos(protocol.TodoUpdate{Patch: patch, Items: []protocol.TodoItem{{ID: "invented", Content: "inspect", Status: "completed", Result: "checked"}}}, func() error { persisted = true; return nil })
		if err == nil || persisted {
			t.Fatalf("patch=%v accepted or persisted an unknown ID", patch)
		}
		restored, err := m.ApplyTodos(protocol.TodoUpdate{PlanID: plan.ID, SelectOnly: true}, nil)
		if err != nil || len(restored.Items) != 1 || restored.Items[0] != plan.Items[0] {
			t.Fatalf("invalid update changed plan: %+v %v", restored, err)
		}
	}
}
