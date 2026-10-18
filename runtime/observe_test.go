package runtime

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestObservationOmitsFreeText(t *testing.T) {
	rt := newTestRuntime(&testBot{})
	rt.events.Publish(Event{RunID: "run_private", Type: EventTodosUpdated, Payload: []TodoItem{{Content: "private task text"}}})
	rt.events.Publish(Event{RunID: "run_private", Type: EventRunFailed, Payload: RunResult{Error: "private error text"}})
	detail, ok := rt.ObservedRun("run_private")
	if !ok {
		t.Fatal("missing run")
	}
	data, err := json.Marshal(detail)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "private task text") || strings.Contains(string(data), "private error text") {
		t.Fatalf("observation leaks free text: %s", data)
	}
}

func TestSummarizeRestoredRunRepairsInvertedTimeline(t *testing.T) {
	start := time.Date(2026, 10, 17, 22, 0, 0, 0, time.UTC)
	end := start.Add(-30 * time.Minute) // recorded terminal time, earlier

	run := RunSnapshot{
		ID: "run_old", Status: RunDone, StartedAt: start, FinishedAt: &end,
		UpdatedAt: end,
	}
	out := SummarizeObservedRun(run)
	if out.FinishedAt != nil {
		t.Fatalf("inconsistent end must be dropped, got %v", out.FinishedAt)
	}
	if out.DurationMS != 0 {
		t.Fatalf("duration = %d, want 0", out.DurationMS)
	}

	// When updated_at is consistent with the start it becomes the real end.
	later := start.Add(2 * time.Minute)
	run.UpdatedAt = later
	out = SummarizeObservedRun(run)
	if out.FinishedAt == nil || !out.FinishedAt.Equal(later) {
		t.Fatalf("finished_at = %v, want %v", out.FinishedAt, later)
	}
	if out.DurationMS != (2 * time.Minute).Milliseconds() {
		t.Fatalf("duration = %d", out.DurationMS)
	}
}

func TestSummarizeRunningRunUsesLiveDuration(t *testing.T) {
	start := time.Now().Add(-3 * time.Second)
	out := SummarizeObservedRun(RunSnapshot{ID: "run_live", Status: RunRunning, StartedAt: start})
	if out.State != ObservedRunning {
		t.Fatalf("state = %s", out.State)
	}
	if out.DurationMS < 2500 || out.DurationMS > 5000 {
		t.Fatalf("duration = %d", out.DurationMS)
	}
}
