package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTaskReviewPreservesFlatStoredReport(t *testing.T) {
	const stored = `{"outcome":"complete","summary":"delivered","checks":[{"name":"entrypoint","status":"passed","limitations":[]}],"unfinished":[],"workspace_path":"/work"}`
	var review TaskReview
	if err := json.Unmarshal([]byte(stored), &review); err != nil {
		t.Fatal(err)
	}
	if review.Summary != "delivered" || len(review.Checks) != 1 || review.WorkspacePath != "/work" {
		t.Fatalf("stored report lost fields: %+v", review)
	}
	encoded, err := json.Marshal(review)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	if fields["summary"] == nil || fields["checks"] == nil || fields["workspace_path"] == nil || strings.Contains(string(encoded), "TaskReport") {
		t.Fatal("report storage acquired an extra nesting level")
	}
}
