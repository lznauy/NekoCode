package budget

import "testing"

func TestConsumeCallCountsExploratoryBash(t *testing.T) {
	q := ToolQuota{MaxSlots: 1}
	if err := q.ConsumeCall("bash", map[string]any{"command": "cat README.md"}); err != nil {
		t.Fatalf("first exploratory bash should fit quota: %v", err)
	}
	if err := q.ConsumeCall("bash", map[string]any{"command": "ls -la"}); err == nil {
		t.Fatal("second exploratory bash should exceed quota")
	}
}

func TestConsumeCallDoesNotCountVerificationBash(t *testing.T) {
	q := ToolQuota{MaxSlots: 1}
	if err := q.ConsumeCall("bash", map[string]any{"command": "go test ./..."}); err != nil {
		t.Fatalf("verification bash should not consume read quota: %v", err)
	}
	if q.Used != 0 {
		t.Fatalf("verification bash consumed quota: %d", q.Used)
	}
}
