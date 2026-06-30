package budget

import "testing"

func TestExplorationRecordCallUsesBashSemantics(t *testing.T) {
	tracker := NewExplorationTracker()

	tracker.RecordCall("bash", map[string]any{"command": "go test ./bot/..."})
	if tracker.Score != MaxScore {
		t.Fatalf("verification bash should not reduce exploration score: got %d", tracker.Score)
	}

	tracker.RecordCall("bash", map[string]any{"command": "rg -n Foo bot"})
	if tracker.Score >= MaxScore {
		t.Fatalf("exploratory bash should reduce exploration score: got %d", tracker.Score)
	}
}
