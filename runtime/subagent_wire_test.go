package runtime

import (
	"encoding/json"
	"testing"
)

func TestSubagentAttemptSummaryAlwaysExportsSummary(t *testing.T) {
	raw, err := json.Marshal(SubagentAttemptSummary{})
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	if string(fields["summary"]) != `{"step_count":0,"stop_reason":""}` {
		t.Fatalf("summary differs from generated required type: %s", raw)
	}
}
