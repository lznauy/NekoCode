package agent

import (
	"testing"

	"nekocode/bot/provider/types"
	"nekocode/protocol"
)

func TestRunPublishesActualSummary(t *testing.T) {
	a, llm := newTestAgentWithLLM(types.StreamToken{Content: "hello"})
	var summaries []protocol.RunSummary
	result := a.Run("hi", func(event protocol.StepEvent) {
		if event.Summary != nil {
			summaries = append(summaries, *event.Summary)
		}
	})
	if result.Error != nil || result.StopReason != "completed" || result.Steps != llm.calls || len(summaries) != 1 {
		t.Fatalf("result=%+v calls=%d summaries=%+v", result, llm.calls, summaries)
	}
	if summaries[0].StepCount != result.Steps || summaries[0].StopReason != result.StopReason {
		t.Fatalf("summary=%+v result=%+v", summaries[0], result)
	}
}
