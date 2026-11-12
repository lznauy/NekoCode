package protocol

import "testing"

func TestTodoReconciliationKeepsIdentityAndUnfinishedItems(t *testing.T) {
	plan, err := ReconcileTodos(nil, []TodoItem{{Content: "inspect", Status: "in_progress"}, {Content: "verify", Status: "pending"}})
	if err != nil {
		t.Fatal(err)
	}
	if plan[0].ID == "" || plan[0].ID == plan[1].ID {
		t.Fatal("step IDs missing or duplicated")
	}
	renamed := append([]TodoItem(nil), plan...)
	renamed[0].Content = "inspect relevant code"
	renamed[0].Status = "completed"
	renamed[0].Result = "reviewed files"
	next, err := ReconcileTodos(plan, renamed)
	if err != nil || next[0].ID != plan[0].ID {
		t.Fatalf("identity changed on rename: %+v %v", next, err)
	}
	if _, err = ReconcileTodos(plan, plan[:1]); err == nil {
		t.Fatal("unfinished item silently disappeared")
	}
	if _, err = ReconcileTodos(plan, nil); err == nil {
		t.Fatal("plan cleared while unfinished")
	}
}
func TestTodoReconciliationRejectsInvalidTransitions(t *testing.T) {
	for _, items := range [][]TodoItem{
		{{Content: "a", Status: "unknown"}},
		{{Content: "a", Status: "completed"}},
		{{Content: "a", Status: "blocked"}},
		{{Content: "a", Status: "cancelled"}},
		{{Content: "a", Status: "in_progress"}, {Content: "b", Status: "in_progress"}},
		{{Content: "a", Status: "pending", ID: "invented"}},
	} {
		if _, err := ReconcileTodos(nil, items); err == nil {
			t.Fatalf("invalid plan accepted: %+v", items)
		}
	}
	old := []TodoItem{{ID: "a", Content: "a", Status: "completed", Result: "done"}}
	if _, err := ReconcileTodos(old, []TodoItem{{ID: "a", Content: "a", Status: "pending"}}); err == nil {
		t.Fatal("settled step reopened without reason")
	}
	old[0].Status = "pending"
	if _, err := ReconcileTodos(old, []TodoItem{old[0], old[0]}); err == nil {
		t.Fatal("duplicate ID accepted")
	}
}
func TestLegacyTodoIdentityAndExplicitDispositions(t *testing.T) {
	old := []TodoItem{{Content: "inspect", Status: "pending"}, {Content: "verify", Status: "pending"}}
	next, err := ReconcileTodos(old, []TodoItem{
		{Content: "inspect", Status: "completed", Result: "files inspected"},
		{Content: "verify", Status: "blocked", Reason: "test environment unavailable"},
	})
	if err != nil || next[0].ID == "" || next[1].ID == "" {
		t.Fatalf("legacy plan not upgraded: %+v %v", next, err)
	}
	next[1].Status = "cancelled"
	next[1].Reason = "user cancelled verification"
	if _, err = ReconcileTodos(next, next); err != nil {
		t.Fatal(err)
	}
}
