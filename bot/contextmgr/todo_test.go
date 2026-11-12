package contextmgr

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"nekocode/protocol"
)

func TestTodoSaveFailureRollsBackOnlyPlanState(t *testing.T) {
	m := New(Config{})
	m.SelectTodoGroup("A")
	first, err := m.ApplyTodos(protocol.TodoUpdate{Items: []protocol.TodoItem{{Content: "work", Status: "pending"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	before := m.Snapshot()
	failure := errors.New("disk full")
	changed := append([]protocol.TodoItem(nil), first.Items...)
	changed[0].Status = "completed"
	changed[0].Result = "done"
	_, err = m.ApplyTodos(protocol.TodoUpdate{Items: changed}, func() error {
		if !m.Status().TasksDone {
			t.Fatal("persistence did not observe new plan")
		}
		m.Add("user", "independent history fact")
		return failure
	})
	if !errors.Is(err, failure) || !reflect.DeepEqual(before.TodoPlans, m.Snapshot().TodoPlans) || m.Status().TasksDone {
		t.Fatalf("failed plan commit leaked: %v", err)
	}
	if m.Len() != 1 {
		t.Fatal("rollback erased unrelated context")
	}
}
func TestTodoUpdatesSerializeAndCannotEraseEachOther(t *testing.T) {
	m := New(Config{})
	m.SelectTodoGroup("A")
	var wg sync.WaitGroup
	var success atomic.Int32
	for _, content := range []string{"first", "second"} {
		wg.Go(func() {
			_, err := m.ApplyTodos(protocol.TodoUpdate{Items: []protocol.TodoItem{{Content: content, Status: "pending"}}}, nil)
			if err == nil {
				success.Add(1)
			}
		})
	}
	wg.Wait()
	if success.Load() != 1 || len(m.Todos()) != 1 {
		t.Fatal("concurrent replacement lost unfinished work")
	}
}
func TestTodoResumeSelectsSavedPlanWithoutChangingOtherDemand(t *testing.T) {
	m := New(Config{})
	m.SelectTodoGroup("A")
	plan, err := m.ApplyTodos(protocol.TodoUpdate{Items: []protocol.TodoItem{{Content: "A work", Status: "pending"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	m.SelectTodoGroup("B")
	if m.Status().HasTasks {
		t.Fatal("new demand inherited A")
	}
	resumed, err := m.ApplyTodos(protocol.TodoUpdate{PlanID: "A", SelectOnly: true}, nil)
	if err != nil || !reflect.DeepEqual(resumed, plan) {
		t.Fatalf("saved plan not restored: %+v %v", resumed, err)
	}
	if _, err = m.ApplyTodos(protocol.TodoUpdate{PlanID: "unknown", SelectOnly: true}, nil); err == nil {
		t.Fatal("unknown plan selected")
	}
}

func TestSelectingLegacyPlanMigratesDuplicateDescriptions(t *testing.T) {
	m := New(Config{})
	m.SelectTodoGroup("legacy")
	m.SetTodos([]protocol.TodoItem{{Content: "review", Status: "pending"}, {Content: "review", Status: "pending"}})
	saved := 0
	plan, err := m.ApplyTodos(protocol.TodoUpdate{PlanID: "legacy", SelectOnly: true}, func() error { saved++; return nil })
	if err != nil || saved != 1 || plan.Items[0].ID == "" || plan.Items[0].ID == plan.Items[1].ID {
		t.Fatalf("legacy IDs not migrated: %+v %v", plan, err)
	}
	items := append([]protocol.TodoItem(nil), plan.Items...)
	items[0].Status = "cancelled"
	items[0].Reason = "duplicate step"
	updated, err := m.ApplyTodos(protocol.TodoUpdate{Items: items}, nil)
	if err != nil || updated.Items[1].ID != plan.Items[1].ID {
		t.Fatalf("migrated steps cannot be updated: %+v %v", updated, err)
	}
}

func TestTodoFailedSaveCannotResurrectPlanAfterResetOrRestore(t *testing.T) {
	for _, restore := range []bool{false, true} {
		t.Run(fmt.Sprint(restore), func(t *testing.T) {
			m := New(Config{})
			m.SelectTodoGroup("old")
			entered, release, updated, reset := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
			go func() {
				defer close(updated)
				_, _ = m.ApplyTodos(protocol.TodoUpdate{Items: []protocol.TodoItem{{Content: "old work", Status: "pending"}}}, func() error {
					close(entered)
					<-release
					return errors.New("disk full")
				})
			}()
			<-entered
			go func() {
				defer close(reset)
				if restore {
					m.Restore(ManagerSnapshot{TodoGroup: "new", Todos: []protocol.TodoItem{{Content: "new work", Status: "pending"}}})
				} else {
					m.Reset()
				}
			}()
			// The lifecycle operation must wait for the plan transaction.
			select {
			case <-reset:
				close(release)
				<-updated
				t.Fatal("lifecycle changed during an uncommitted save")
			case <-time.After(20 * time.Millisecond):
			}
			close(release)
			<-updated
			<-reset
			if restore {
				if m.ActiveTodoPlanID() != "new" || m.Todos()[0].Content != "new work" {
					t.Fatal("failed save replaced restored plan")
				}
			} else if m.Status().HasTasks || m.ActiveTodoPlanID() != "" {
				t.Fatal("failed save resurrected reset plan")
			}
		})
	}
}
