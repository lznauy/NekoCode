package protocol

import "testing"

func TestTodoPatchUpdatesOneStepWithoutDroppingUnfinishedWork(t *testing.T) {
	old, err := ReconcileTodos(nil, []TodoItem{{Content: "inspect", Status: "in_progress"}, {Content: "verify", Status: "pending"}})
	if err != nil {
		t.Fatal(err)
	}
	next, err := PatchTodos(old, []TodoItem{{ID: old[0].ID, Status: "completed", Result: "found cause"}, {ID: old[1].ID, Status: "in_progress"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(next) != 2 || next[0].Content != "inspect" || next[1].ID != old[1].ID || old[0].Status != "in_progress" {
		t.Fatalf("patch lost or mutated work: %+v / %+v", old, next)
	}
	parallel, err := PatchTodos(old, []TodoItem{{ID: old[1].ID, Status: "in_progress"}})
	if err != nil || len(parallel) != 2 || parallel[0].Status != "in_progress" || parallel[1].Status != "in_progress" {
		t.Fatalf("independent parallel steps rejected: %+v %v", parallel, err)
	}
	for _, bad := range [][]TodoItem{
		{{ID: "invented", Status: "pending"}},
		{{ID: old[0].ID, Status: "completed"}},
		{{ID: old[0].ID}, {ID: old[0].ID}},
	} {
		if _, err := PatchTodos(old, bad); err == nil {
			t.Fatalf("invalid patch admitted: %+v", bad)
		}
	}
	if _, err = PatchTodos(next, []TodoItem{{ID: next[0].ID, Status: "pending"}}); err == nil {
		t.Fatal("reopening completed work without reason accepted")
	}
}
