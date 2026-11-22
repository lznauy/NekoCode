package subagent

import (
	"testing"
	"testing/synctest"
	"time"

	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/extension/tool"
	"nekocode/bot/provider/types"
)

func TestForegroundDeadlinePreservesSubmittedHandoff(t *testing.T) {
	for _, tc := range []struct {
		name       string
		script     []types.StreamToken
		status     Status
		stopReason string
	}{
		{"completed", completionScript(), StatusCompleted, "completed"},
		{"partial", stageHandoff(), StatusPartial, "incomplete"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				llm := &scriptedLLM{scripts: [][]types.StreamToken{tc.script}}
				var savedStatus, savedReason string
				result, err := New(Config{LLM: llm, Tools: tools.New()}).Run(t.Context(), RunConfig{
					ForegroundInterval: time.Minute,
					OnPhase: func(phase string) {
						if phase == "done" {
							// Expire the scheduling interval after the handoff has passed validation.
							time.Sleep(2 * time.Minute)
						}
					},
					SaveContext: func(_ ctxmgr.ManagerSnapshot, status, _ string, usage AttemptUsage) error {
						savedStatus, savedReason = status, usage.Summary.StopReason
						return nil
					},
				})
				if err != nil || result == nil || result.Handoff == nil || result.Status != tc.status || result.Summary.StopReason != tc.stopReason {
					t.Fatalf("submitted handoff lost or misclassified: %+v, err=%v", result, err)
				}
				if savedStatus != tc.name || savedReason != tc.stopReason {
					t.Fatalf("checkpoint status=%q reason=%q", savedStatus, savedReason)
				}
			})
		})
	}
}
