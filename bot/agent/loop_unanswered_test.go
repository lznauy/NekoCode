package agent

import (
	"testing"

	"nekocode/bot/provider/types"
)

// A run whose LLM never answers (misconfigured model, dead endpoint) must
// not strand the user message in the context: the next attempt would send
// both copies together.
func TestFailedRunDoesNotStrandUserMessage(t *testing.T) {
	a := newTestAgent()
	llm := &failingLLM{}
	a.deps.llmClient = llm

	result := a.Run("broken question", nil, nil)
	if result.FinalOutput == "" {
		t.Fatalf("run produced no fallback output: %+v", result)
	}
	snapshot := a.deps.ctxMgr.Snapshot()
	for _, m := range snapshot.Messages {
		if m.Role == "user" && m.Content == "broken question" {
			t.Fatal("unanswered user message was left stranded in the context")
		}
	}
	if llm.calls == 0 {
		t.Fatal("the LLM was never called")
	}
}

// A successful run keeps both the user message and the assistant answer.
func TestSuccessfulRunKeepsTurnInContext(t *testing.T) {
	a, _ := newTestAgentWithLLM(types.StreamToken{Content: "hello"})
	result := a.Run("real question", nil, nil)
	if result.Error != nil {
		t.Fatalf("run error: %v", result.Error)
	}
	snapshot := a.deps.ctxMgr.Snapshot()
	var hasUser, hasAssistant bool
	for _, m := range snapshot.Messages {
		if m.Role == "user" && m.Content == "real question" {
			hasUser = true
		}
		if m.Role == "assistant" && m.Content == "hello" {
			hasAssistant = true
		}
	}
	if !hasUser || !hasAssistant {
		t.Fatalf("answered turn was rolled back: user=%v assistant=%v", hasUser, hasAssistant)
	}
}
