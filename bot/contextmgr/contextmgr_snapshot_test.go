package contextmgr

import (
	"nekocode/protocol"
	"testing"

	"nekocode/bot/contextmgr/token"
)

func TestSnapshotRestore(t *testing.T) {
	first := New(Config{SystemPrompt: "test prompt"})
	first.ConfigureModel(ModelContext{Window: 1_000_000})
	first.Add("user", "hello world")
	first.state.tracker.RecordPrompt(10_000)
	first.state.tracker.RecordCache(9_000, 1_000)
	first.state.tracker.RecordSubagent(300, 200, 100)
	first.state.compactCount = 2
	first.state.trimCount = 40

	snap := first.Snapshot()

	second := New(Config{SystemPrompt: "test prompt"})
	second.ConfigureModel(ModelContext{Window: 128_000})
	second.state.tracker.RecordSubagent(999, 999, 0)
	second.state.compactCount = 7
	second.Restore(snap)

	if got, want := len(second.Snapshot().Messages), len(first.Snapshot().Messages); got != want {
		t.Errorf("restored len = %d, want %d", got, want)
	}
	if got, want := len(second.Snapshot().Transcript), len(first.Snapshot().Transcript); got != want {
		t.Errorf("restored transcript len = %d, want %d", got, want)
	}
	if budget := second.Status().Budget; budget != 128_000 {
		t.Errorf("restored budget = %d, want active model budget 128000", budget)
	}
	tracker := second.state.tracker.Snapshot()
	// Cumulative cache hit/miss is a session-scoped diagnostic (it feeds the
	// /context "Session" line), so it survives a reload. The provider-calibrated
	// prompt count does not: the session may be resumed on another model.
	if tracker.CacheHitTokens != 9_000 || tracker.CacheMissTokens != 1_000 {
		t.Errorf("restored cache counters = %+v, want 9000/1000", tracker)
	}
	if tracker.LastPromptTokens != 0 {
		t.Errorf("restored prompt calibration = %d, want 0", tracker.LastPromptTokens)
	}
	if tracker.Sub != (token.SubStats{Count: 1, TotalTokens: 300, CacheHitTokens: 200, CacheMissTokens: 100}) {
		t.Errorf("restored sub-agent stats = %+v, want snapshot totals", tracker.Sub)
	}
	// Compaction counters describe the restored archive; zeroing them would
	// report an archive that no compaction produced.
	if got := second.Report(); got.CompactCount != 2 || got.Archived != 40 {
		t.Errorf("restored compaction counters = %d compactions / %d trimmed, want 2/40", got.CompactCount, got.Archived)
	}
}

func TestTaskUpdateReceiptSurvivesArchivedContext(t *testing.T) {
	manager := New(Config{})
	// The message containing sequence 9 has already been compacted into archive.
	manager.Restore(ManagerSnapshot{Archive: "prior user instructions", TaskUpdateSequence: 9})
	manager.AddTaskUpdate(9, "do not repeat the archived instruction")
	if len(manager.Snapshot().Messages) != 0 {
		t.Fatal("archived update reinserted")
	}
	manager.AddTaskUpdate(10, "new instruction")
	snap := manager.Snapshot()
	if snap.TaskUpdateSequence != 10 || len(snap.Messages) != 1 || snap.Messages[0].Content != "new instruction" {
		t.Fatalf("wrong receipt: %+v", snap)
	}
}

func TestTodoSnapshotRestoresWithoutAliasingOrSessionLeak(t *testing.T) {
	first := New(Config{})
	first.SetTodos([]protocol.TodoItem{{Content: "verify", Status: "pending"}})
	snap := first.Snapshot()
	snap.Todos[0].Content = "copy"
	if first.Todos()[0].Content != "verify" {
		t.Fatal("snapshot aliases live plan")
	}
	second := New(Config{})
	second.Restore(snap)
	snap.Todos[0].Content = "changed"
	if second.Todos()[0].Content != "copy" || second.Status().TasksDone {
		t.Fatal("restored plan lost or aliased")
	}
	second.Restore(ManagerSnapshot{})
	if second.Status().HasTasks {
		t.Fatal("previous session plan leaked")
	}
}

func TestInterleavedDemandPlansSurviveRestore(t *testing.T) {
	m := New(Config{})
	m.SelectTodoGroup("A")
	m.SetTodos([]protocol.TodoItem{{Content: "A fix", Status: "pending"}})
	m.SelectTodoGroup("B")
	m.SetTodos([]protocol.TodoItem{{Content: "B verification", Status: "in_progress"}})
	snap := m.Snapshot()
	restored := New(Config{})
	restored.Restore(snap)
	restored.SelectTodoGroup("A")
	if got := restored.Todos(); len(got) != 1 || got[0].Content != "A fix" {
		t.Fatalf("A plan replaced by B: %+v", got)
	}
	restored.SetTodos([]protocol.TodoItem{{Content: "A fix", Status: "completed"}})
	restored.SelectTodoGroup("B")
	if got := restored.Todos(); len(got) != 1 || got[0].Status != "in_progress" {
		t.Fatalf("A completion changed B: %+v", got)
	}
	restored.SelectTodoGroup("new")
	if restored.Status().HasTasks {
		t.Fatal("new demand inherited an unrelated plan")
	}
}
