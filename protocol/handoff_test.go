package protocol

import "testing"

func TestHandoffContractCoversLegacyAndExplicitCompletion(t *testing.T) {
	for _, outcome := range []string{"", "continue", "blocked", "decision"} {
		if !HandoffIncomplete(outcome, []string{"remaining"}) {
			t.Fatal("unfinished task became complete", outcome)
		}
	}
	if HandoffIncomplete("complete", nil) || HandoffIncomplete("", nil) {
		t.Fatal("finished handoff marked incomplete")
	}
	for _, value := range []struct {
		outcome, next string
		work          []string
		valid         bool
	}{{"", "", []string{"remaining"}, true}, {"complete", "", nil, true}, {"complete", "", []string{"remaining"}, false}, {"continue", "next", []string{"remaining"}, true}, {"continue", "", []string{"remaining"}, false}, {"unknown", "next", nil, false}} {
		if (ValidateHandoff(value.outcome, value.next, value.work) == nil) != value.valid {
			t.Fatalf("invalid handoff validation: %+v", value)
		}
	}
}
