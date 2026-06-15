package hooks

import (
	"strings"
	"testing"
)

func TestEmptyRegistry(t *testing.T) {
	r := NewRegistry()
	if r == nil {
		t.Fatal("NewRegistry returned nil")
	}
	if len(r.List()) != 0 {
		t.Error("expected empty hook list")
	}
}

func TestFormatHints(t *testing.T) {
	hints := []Hint{
		{Type: "quota", Severity: "warning", Content: "one"},
		{Type: "verification", Severity: "critical", Content: "two"},
	}
	s := FormatHints(hints)
	if !strings.Contains(s, `<hints>`) {
		t.Error("missing hints wrapper")
	}
	if !strings.Contains(s, "type=\"quota\"") {
		t.Error("missing quota hint")
	}
	if !strings.Contains(s, "type=\"verification\"") {
		t.Error("missing verification hint")
	}
}

func TestQuotaHook(t *testing.T) {
	hk := quotaHook()
	snap := &Snapshot{Store: make(map[string]int64)}

	snap.set(StoreQuotaReads, 5)
	if r := hk.On(snap); r != nil {
		t.Error("reads=5 -> silent")
	}

	snap.set(StoreQuotaReads, 2)
	r := hk.On(snap)
	if r == nil || r.Hint == nil {
		t.Fatal("reads=2 -> fire")
	}
	if r.Hint.Severity != "warning" {
		t.Errorf("expected warning, got %s", r.Hint.Severity)
	}

	// Dedup: same level should stay silent
	snap.set(StoreQuotaReads, 2)
	if r := hk.On(snap); r != nil {
		t.Error("reads=2 again -> silent (dedup)")
	}

	// Different level → fire again
	snap.set(StoreQuotaReads, 1)
	r = hk.On(snap)
	if r == nil || r.Hint == nil {
		t.Fatal("reads=1 -> fire (level changed)")
	}

	snap.set(StoreQuotaReads, 0)
	r = hk.On(snap)
	if r == nil || r.Hint == nil || r.Hint.Severity != "critical" {
		t.Fatal("reads=0 -> critical")
	}
}

func TestVerificationHook(t *testing.T) {
	hk := verificationHook()
	snap := &Snapshot{Store: make(map[string]int64)}

	// No tasks -> silent
	snap.set(StoreHasTasks, 0)
	if r := hk.On(snap); r != nil {
		t.Error("no tasks -> silent")
	}

	// Has tasks, all done -> silent
	snap.set(StoreHasTasks, 1)
	snap.set(StoreTasksAllDone, 1)
	if r := hk.On(snap); r != nil {
		t.Error("all tasks done -> silent")
	}

	// Has tasks, not done, but has tool calls -> silent
	snap.set(StoreTasksAllDone, 0)
	snap.set(StoreTurnToolCalls, 2)
	if r := hk.On(snap); r != nil {
		t.Error("has tool calls -> silent")
	}

	// Has tasks, not done, no tool calls -> fire
	snap.set(StoreTurnToolCalls, 0)
	r := hk.On(snap)
	if r == nil || r.Hint == nil {
		t.Fatal("unfinished tasks, no tools -> fire")
	}
	if r.Hint.Type != "verification" {
		t.Errorf("expected verification, got %s", r.Hint.Type)
	}

	// Dedup: already fired -> silent
	if r := hk.On(snap); r != nil {
		t.Error("already fired -> silent (dedup)")
	}

	// Reset when tasks all done
	snap.set(StoreTasksAllDone, 1)
	if r := hk.On(snap); r != nil {
		t.Error("all done resets dedup -> silent")
	}
}

func TestGarbledCircuitBreaker(t *testing.T) {
	hk := garbledCircuitBreaker()
	snap := &Snapshot{Store: make(map[string]int64)}

	snap.set(StoreRespGarbled, 4)
	if r := hk.On(snap); r != nil {
		t.Error("count=4 -> no stop")
	}

	snap.set(StoreRespGarbled, 5)
	r := hk.On(snap)
	if r == nil || r.Stop == nil {
		t.Fatal("count=5 -> stop")
	}
	if *r.Stop != StopFormatError {
		t.Errorf("expected format_error, got %s", *r.Stop)
	}
}

func TestResetSession(t *testing.T) {
	r := NewRegistry()
	r.Set(StoreFileModified, 1)
	r.Set(StoreQuotaReads, 5)

	r.ResetSession()
	if r.store[StoreFileModified] != 0 || r.store[StoreQuotaReads] != 0 {
		t.Error("store should be empty after session reset")
	}
}

func TestCompletionQualityHook(t *testing.T) {
	hk := completionQualityHook()
	snap := &Snapshot{Store: make(map[string]int64)}
	snap.set(StoreStepInputLen, 100)

	// No tasks (StoreHasTasks=0) → silent, resets guard
	snap.set(StoreTasksAllDone, 0)
	snap.set(StoreHasTasks, 0)
	if r := hk.On(snap); r != nil {
		t.Error("no tasks → silent")
	}

	// Tasks done but no has_tasks flag → silent
	snap.set(StoreTasksAllDone, 1)
	snap.set(StoreHasTasks, 0)
	snap.set(StoreFileModified, 0)
	if r := hk.On(snap); r != nil {
		t.Error("tasks done + no tasks → silent")
	}

	// Tasks done, file modified → silent (sets guard)
	snap.set(StoreTasksAllDone, 1)
	snap.set(StoreHasTasks, 1)
	snap.set(StoreFileModified, 1)
	if r := hk.On(snap); r != nil {
		t.Error("tasks done + modified → silent")
	}
	if !snap.flag("flag:quality_warned") {
		t.Error("guard should be set")
	}

	// Guard already set → silent
	snap.set(StoreFileModified, 0)
	if r := hk.On(snap); r != nil {
		t.Error("guard set → silent")
	}

	// Fresh snapshot: has tasks, all done, no files modified, has tool calls → silent (analysis task)
	fresh := &Snapshot{Store: make(map[string]int64)}
	fresh.set(StoreStepInputLen, 100)
	fresh.set(StoreTasksAllDone, 1)
	fresh.set(StoreHasTasks, 1)
	fresh.set(StoreFileModified, 0)
	fresh.set(StoreTurnToolCalls, 2)
	if r := hk.On(fresh); r != nil {
		t.Error("tasks done + tool calls (analysis) → silent")
	}

	// Fresh snapshot: has tasks, all done, no files modified, no tool calls → fire
	fresh2 := &Snapshot{Store: make(map[string]int64)}
	fresh2.set(StoreStepInputLen, 100)
	fresh2.set(StoreTasksAllDone, 1)
	fresh2.set(StoreHasTasks, 1)
	fresh2.set(StoreFileModified, 0)
	fresh2.set(StoreTurnToolCalls, 0)
	r := hk.On(fresh2)
	if r == nil || r.Hint == nil {
		t.Fatal("tasks done + no modify + no tools → fire")
	}
	if r.Hint.Type != "quality" {
		t.Errorf("expected quality, got %s", r.Hint.Type)
	}

	// Trivial input (len < 6) → silent
	trivial := &Snapshot{Store: make(map[string]int64)}
	trivial.set(StoreStepInputLen, 2)  // "你好"
	trivial.set(StoreTasksAllDone, 1)
	trivial.set(StoreHasTasks, 1)
	trivial.set(StoreFileModified, 0)
	if r := hk.On(trivial); r != nil {
		t.Error("trivial input len=2 → silent")
	}
}

func TestExplorationExhaustedHook(t *testing.T) {
	hk := explorationExhaustedHook()
	snap := &Snapshot{
		Store:   make(map[string]int64),
		strVals: make(map[string]string),
	}
	snap.strVals[StoreStepInput] = "test task"

	// No exploration calls yet → silent (first turn)
	snap.set(StoreExploreCalls, 0)
	snap.set(StoreExploreScore, 0)
	if r := hk.On(snap); r != nil {
		t.Error("no exploration yet → silent")
	}

	// Few exploration calls → silent (not enough)
	snap.set(StoreExploreCalls, 5)
	snap.set(StoreExploreScore, 0)
	if r := hk.On(snap); r != nil {
		t.Error("few calls → silent")
	}

	// Exploration happened, score > 0 → silent (still have budget)
	snap.set(StoreExploreCalls, 10)
	snap.set(StoreExploreScore, 3)
	if r := hk.On(snap); r != nil {
		t.Error("exploration with budget → silent")
	}

	// Exploration happened (>= 10 calls), score = 0 → fire (budget exhausted)
	snap.set(StoreExploreScore, 0)
	r := hk.On(snap)
	if r == nil || r.Hint == nil {
		t.Fatal("exploration exhausted → fire")
	}
	if r.Hint.Type != "exploration" {
		t.Errorf("expected exploration, got %s", r.Hint.Type)
	}

	// Dedup → silent
	if r := hk.On(snap); r != nil {
		t.Error("already fired → silent (dedup)")
	}
}

func TestExploreCascadeHook(t *testing.T) {
	hk := exploreCascadeHook()
	snap := &Snapshot{
		Store:   make(map[string]int64),
		strVals: make(map[string]string),
	}
	snap.strVals[StoreStepInput] = "test task"

	// 0 researchers → silent
	snap.set(StoreToolResearcher, 0)
	if r := hk.On(snap); r != nil {
		t.Error("0 researchers → silent")
	}

	// 3 researchers → silent
	snap.set(StoreToolResearcher, 3)
	if r := hk.On(snap); r != nil {
		t.Error("3 researchers → silent")
	}

	// 4 researchers → fire
	snap.set(StoreToolResearcher, 4)
	r := hk.On(snap)
	if r == nil || r.Hint == nil {
		t.Fatal("4 researchers → fire")
	}
	if r.Hint.Type != "explore_cascade" {
		t.Errorf("expected explore_cascade, got %s", r.Hint.Type)
	}

	// 5 researchers → fire
	snap.set(StoreToolResearcher, 5)
	r = hk.On(snap)
	if r == nil || r.Hint == nil {
		t.Fatal("5 researchers → fire")
	}
}

func TestToolIdleHook(t *testing.T) {
	hk := toolIdleHook()
	snap := &Snapshot{
		Store:   make(map[string]int64),
		strVals: make(map[string]string),
	}
	snap.strVals[StoreStepInput] = "test task"

	// Turn with edits → reset idle counter
	snap.set(StoreHasEdits, 1)
	snap.set(StoreTurnToolCalls, 3)
	if r := hk.On(snap); r != nil {
		t.Error("edits turn → silent (resets counter)")
	}
	if c := snap.get("counter:idle_calls"); c != 0 {
		t.Errorf("idle counter reset: expected 0, got %d", c)
	}

	// First idle turn → counter=2, no fire
	snap.set(StoreHasEdits, 0)
	snap.set(StoreTurnToolCalls, 2)
	if r := hk.On(snap); r != nil {
		t.Error("first idle turn → silent")
	}
	if c := snap.get("counter:idle_calls"); c != 2 {
		t.Errorf("expected 2, got %d", c)
	}

	// Run up to 48 calls → still no fire (need 50)
	for i := 0; i < 23; i++ {
		if r := hk.On(snap); r != nil {
			t.Fatalf("idle call %d → should be silent", i)
		}
	}

	// 50th call → fire
	r := hk.On(snap)
	if r == nil || r.Hint == nil {
		t.Fatal("50th idle call → fire")
	}
	if r.Hint.Type != "idle" {
		t.Errorf("expected idle, got %s", r.Hint.Type)
	}

	// After fire, dedup → silent
	if r := hk.On(snap); r != nil {
		t.Error("after fire → silent (dedup)")
	}

	// Edits turn resets both counter and dedup flag
	snap.set(StoreHasEdits, 1)
	if r := hk.On(snap); r != nil {
		t.Error("edits resets dedup")
	}

	// After reset, can fire again on new idle streak
	snap.set(StoreHasEdits, 0)
	snap.set(StoreTurnToolCalls, 5)
	// idle count: 5, 10, ..., 45 (silent), 50 (fire)
	for i := 0; i < 9; i++ {
		if r := hk.On(snap); r != nil {
			t.Errorf("idle call %d → silent", i+1)
		}
	}
	// 50th call → fire
	snap.set(StoreHasEdits, 0)
	r = hk.On(snap)
	if r == nil || r.Hint == nil {
		t.Fatal("new idle streak 50th call → fire")
	}
	if r.Hint.Type != "idle" {
		t.Errorf("expected idle, got %s", r.Hint.Type)
	}

	// Turn with no tool calls → silent
	noTools := &Snapshot{Store: make(map[string]int64)}
	noTools.set(StoreHasEdits, 0)
	noTools.set(StoreTurnToolCalls, 0)
	if r := hk.On(noTools); r != nil {
		t.Error("no tool calls → silent")
	}
}
