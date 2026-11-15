package runtime

import (
	"strings"
	"testing"
)

func TestIncompleteStructuredReviewRequiresCanonicalRead(t *testing.T) {
	report := cleanReviewFixture()
	report.Evidence = []string{strings.Repeat("decisive evidence ", 800)}
	preview, complete := boundedTaskReview(report)
	canonical := "full findings; counterevidence: next request uses cache; actual checks blocked"
	task := BackgroundTask{ID: "task", SessionID: "session", GroupID: "group", AttemptID: "attempt", Status: "partial", Review: preview, ReviewComplete: complete, Result: canonical}
	b := &backgroundTasks{entries: map[string]*backgroundEntry{"task": {BackgroundTask: task}}, order: []string{"task"}}
	got := b.reviewLocked("session", "group", []string{"task"}).Results[0]
	if got.Result != "" || got.ResultComplete || got.ReadResult == nil {
		t.Fatalf("incomplete report silently accepted unrelated text: %+v", got)
	}
}
