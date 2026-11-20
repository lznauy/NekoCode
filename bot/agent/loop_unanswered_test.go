package agent

import (
	"context"
	"errors"
	"nekocode/bot/contextmgr"
	"strings"
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
	if result.FinalOutput != "" || !errors.Is(result.Error, context.DeadlineExceeded) || result.StopReason != "execution_error" {
		t.Fatalf("model failure reported as completed: %+v", result)
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

// The synthesize fallback is not a model response and must not be persisted:
// recording it would mark an unanswered run as answered and strand the user
// message even after the rollback fix.
func TestSynthesizeFallbackIsNotPersisted(t *testing.T) {
	a := newTestAgent()
	llm := &failingLLM{}
	a.deps.llmClient = llm
	before := a.deps.ctxMgr.Len()

	output := a.modelRunner.synthesize()

	if output != fallbackSynthesize {
		t.Fatalf("output = %q, want the fixed fallback", output)
	}
	if got := a.deps.ctxMgr.Len(); got != before {
		t.Fatalf("fallback was persisted: context grew from %d to %d", before, got)
	}
}

func TestFailedRunAfterCompactionRemovesUnansweredInput(t *testing.T) {
	for _, durable := range []bool{false, true} {
		a := newTestAgent()
		summaries := 0
		m := contextmgr.New(contextmgr.Config{ContextWindow: 16384, AutoCompactPercent: 1, Summarizer: func([]types.Message, string) (string, error) {
			summaries++
			return "<summary>Previous work.</summary>", nil
		}})
		a.deps.ctxMgr = m
		a.deps.llmClient = &failingLLM{}
		for range 8 {
			m.Add("user", strings.Repeat("previous ", 30))
			m.Add("assistant", "previous answer")
		}
		cursor := 0
		if durable {
			m.SetBeforeCompaction(func(string) error {
				base, tail, _ := m.TranscriptTail()
				cursor = base + len(tail)
				m.AcknowledgeTranscript(base, cursor)
				return nil
			})
		}
		result := a.Run("currently unanswered", nil, nil)
		after := m.Snapshot()
		if summaries != 1 || result.Error == nil || containsMessage(after.Messages, "currently unanswered") || containsMessage(after.Transcript, "currently unanswered") || after.TranscriptBase != cursor {
			t.Fatalf("durable=%t summaries=%d result=%+v cursor=%d got=%d unanswered=%t", durable, summaries, result, cursor, after.TranscriptBase, containsMessage(after.Messages, "currently unanswered"))
		}
	}
}
