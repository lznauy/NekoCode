package runstore

import (
	"testing"
	"time"

	"nekocode/runtime/internal/core"
)

func TestRepeatedRejectedResumeEndsActivePlaceholder(t *testing.T) {
	store := NewRunStore(0)
	for range 2 {
		store.Record(core.Event{RunID: "run", Type: core.EventSubAgentStarted, Payload: core.SubAgentPayload{ID: "saved", SessionID: "parent", Status: "queued"}, Time: time.Now()})
		store.Record(core.Event{RunID: "run", Type: core.EventSubAgentEnded, Payload: core.SubAgentPayload{ID: "saved", Status: "not_started"}, Time: time.Now()})
	}
	for _, agent := range store.Subagents("parent") {
		if agent.Active || agent.Status != "not_started" {
			t.Fatalf("rejected resume left a queued task: %+v", agent)
		}
	}
}

func TestQueuedTasksStayQueuedAcrossWaitEventsAndEndWithParent(t *testing.T) {
	store := NewRunStore(0)
	store.Record(core.Event{RunID: "run", Type: core.EventSubAgentStarted, Payload: core.SubAgentPayload{ID: "queued", SessionID: "parent", Status: "queued"}, Time: time.Now()})
	store.Record(core.Event{RunID: "run", Type: core.EventApprovalRequested, Payload: core.ApprovalView{ID: "main-approval", Status: core.ApprovalPending}, Time: time.Now()})
	agents := store.Subagents("parent")
	if len(agents) != 1 || agents[0].Status != "queued" {
		t.Fatalf("queue became running: %+v", agents)
	}
	store.Record(core.Event{RunID: "run", Type: core.EventRunCancelled, Time: time.Now()})
	agents = store.Subagents("parent")
	if agents[0].Active || agents[0].Status != "interrupted" {
		t.Fatalf("cancel left queued task live: %+v", agents)
	}
}

func TestAttemptsBindQueuedPlaceholderAndIgnoreLateOldEvents(t *testing.T) {
	store := NewRunStore(0)
	now := time.Now()
	emit := func(kind core.EventType, payload core.SubAgentPayload) {
		store.Record(core.Event{RunID: "run", Type: kind, Payload: payload, Time: now})
	}
	emit(core.EventSubAgentStarted, core.SubAgentPayload{ID: "child", SessionID: "parent", Status: "queued"})
	emit(core.EventSubAgentStarted, core.SubAgentPayload{ID: "child", SessionID: "parent", AttemptID: "first", AttemptSeq: 1, Status: "running"})
	emit(core.EventSubAgentEnded, core.SubAgentPayload{ID: "child", AttemptID: "first", AttemptSeq: 1, Status: "completed"})
	emit(core.EventSubAgentStarted, core.SubAgentPayload{ID: "child", SessionID: "parent", Status: "queued"})
	emit(core.EventSubAgentStarted, core.SubAgentPayload{ID: "child", SessionID: "parent", AttemptID: "second", AttemptSeq: 2, Status: "running"})
	emit(core.EventSubAgentEnded, core.SubAgentPayload{ID: "child", AttemptID: "first", AttemptSeq: 1, Status: "failed"})
	emit(core.EventSubAgentUpdated, core.SubAgentPayload{ID: "child", AttemptID: "second", AttemptSeq: 2, Status: "running", Phase: "testing"})
	agents := store.Subagents("parent")
	if len(agents) != 2 {
		t.Fatalf("queued placeholders were not bound: %+v", agents)
	}
	for _, agent := range agents {
		switch agent.AttemptID {
		case "first":
			if agent.Status != "failed" {
				t.Fatalf("late old end applied to wrong state: %+v", agent)
			}
		case "second":
			if agent.Status != "running" || agent.Phase != "testing" {
				t.Fatalf("new attempt polluted: %+v", agent)
			}
		default:
			t.Fatalf("unexpected phantom attempt: %+v", agent)
		}
	}
}

func TestSubagentsTrackIndependentWaitsAndKeepParentWaiting(t *testing.T) {
	store := NewRunStore(0)
	now := time.Now()
	emit := func(kind core.EventType, payload any) {
		store.Record(core.Event{RunID: "run", Type: kind, Payload: payload, Time: now})
	}
	for _, id := range []string{"a", "b"} {
		emit(core.EventSubAgentStarted, core.SubAgentPayload{ID: id, SessionID: "parent", Profile: "explore"})
	}
	emit(core.EventApprovalRequested, core.ApprovalView{ID: "approve-a", SubAgentID: "a", Status: core.ApprovalPending})
	emit(core.EventQuestionRequested, core.QuestionView{ID: "ask-b", SubAgentID: "b", Status: core.QuestionPending})
	check := func(a, b string, parent core.RunStatus) {
		t.Helper()
		snapshot, _ := store.Lookup("run")
		if snapshot.Status != parent || snapshot.SubAgents[0].Status != a || snapshot.SubAgents[1].Status != b {
			t.Fatalf("parent=%s agents=%+v", snapshot.Status, snapshot.SubAgents)
		}
	}
	check("waiting_approval", "waiting_question", core.RunWaitingApproval)
	emit(core.EventApprovalResolved, core.ApprovalView{ID: "approve-a", SubAgentID: "a", Status: core.ApprovalApproved})
	check("running", "waiting_question", core.RunWaitingQuestion)
	// A phase update cannot erase an outstanding question.
	emit(core.EventSubAgentUpdated, core.SubAgentPayload{ID: "b", Status: "running", Phase: "Waiting"})
	check("running", "waiting_question", core.RunWaitingQuestion)
	emit(core.EventQuestionResolved, core.QuestionView{ID: "ask-b", SubAgentID: "b", Status: core.QuestionAnswered})
	check("running", "running", core.RunRunning)
	emit(core.EventSubAgentEnded, core.SubAgentPayload{ID: "a", Status: "completed"})
	emit(core.EventSubAgentUpdated, core.SubAgentPayload{ID: "a", Status: "running", Phase: "late"})
	check("completed", "running", core.RunRunning)
	if agents := store.Subagents("other"); len(agents) != 0 {
		t.Fatal("cross-session task leak")
	}
}

func TestParentEndInterruptsUnfinishedChildren(t *testing.T) {
	store := NewRunStore(0)
	store.Record(core.Event{RunID: "run", Type: core.EventSubAgentStarted, Payload: core.SubAgentPayload{ID: "child", SessionID: "parent"}, Time: time.Now()})
	store.Record(core.Event{RunID: "run", Type: core.EventRunCancelled, Time: time.Now()})
	agents := store.Subagents("parent")
	if len(agents) != 1 || agents[0].Active || agents[0].Status != "interrupted" {
		t.Fatalf("orphan live task: %+v", agents)
	}
}

func TestLateWaitRequestDoesNotReopenCompletedRun(t *testing.T) {
	for _, kind := range []core.EventType{core.EventApprovalRequested, core.EventQuestionRequested} {
		store := NewRunStore(0)
		store.Record(core.Event{RunID: "run", Type: core.EventRunDone, Time: time.Now()})
		var payload any = core.ApprovalView{ID: "late", Status: core.ApprovalPending}
		if kind == core.EventQuestionRequested {
			payload = core.QuestionView{ID: "late", Status: core.QuestionPending}
		}
		store.Record(core.Event{RunID: "run", Type: kind, Payload: payload, Time: time.Now()})
		snapshot, _ := store.Lookup("run")
		if snapshot.Status != core.RunDone {
			t.Fatalf("%s reopened completed run: %s", kind, snapshot.Status)
		}
	}
}
