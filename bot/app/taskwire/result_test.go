package taskwire

import (
	"testing"

	"nekocode/bot/agent/subagent"
	"nekocode/bot/tools"
)

func TestToTaskResultMapsStatus(t *testing.T) {
	tests := []struct {
		status subagent.Status
		want   tools.TaskStatus
	}{
		{subagent.StatusCompleted, tools.TaskStatusCompleted},
		{subagent.StatusFailed, tools.TaskStatusFailed},
		{subagent.StatusPartial, tools.TaskStatusPartial},
	}
	for _, tt := range tests {
		got := ToTaskResult(&subagent.Result{Status: tt.status, Content: "ok"})
		if got.Status != tt.want || got.Content != "ok" {
			t.Fatalf("ToTaskResult(%v) = %+v", tt.status, got)
		}
	}
	if ToTaskResult(nil) != nil {
		t.Fatal("nil result should map to nil")
	}
}
