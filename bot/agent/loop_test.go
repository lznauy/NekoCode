package agent

import (
	"context"
	"reflect"
	"strconv"
	"strings"
	"testing"

	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/extension/tool"
	"nekocode/bot/policy"
	"nekocode/bot/policy/builtin"
	"nekocode/bot/provider/types"
)

func TestSmallContextWindowDoesNotTriggerSyntheticBlocking(t *testing.T) {
	llm := &fakeLLM{}
	a := New(context.Background(), Config{
		Context: ctxmgr.New(ctxmgr.Config{
			SystemPrompt:  "test",
			ContextWindow: 1000,
		}),
		Model:  llm,
		Tools:  tools.New(),
		Policy: policy.New(),
	})

	result := a.Run("hello", nil)
	if result.Error != nil {
		t.Fatalf("run error = %v", result.Error)
	}
	if llm.calls != 1 {
		t.Fatalf("model calls = %d, want 1", llm.calls)
	}
}

// TestFinishRunPersistsPolicyBlockedFinalText reproduces the session-reload
// bug: when a Stop hook blocks the final answer via applyFinalPolicyBlock,
// the text was shown to the user (returned as FinalOutput) but never appended
// to the context, so /sessions <id> lost it.
func TestFinishRunPersistsPolicyBlockedFinalText(t *testing.T) {
	ctxMgr := ctxmgr.New(ctxmgr.Config{SystemPrompt: "test", ContextWindow: 128000})
	reg := tools.New()
	gov := policy.New()
	builtin.Register(gov)
	a := New(context.Background(), Config{Context: ctxMgr, Tools: reg, Policy: gov})

	finalText := "• P0-1 Bash 安全机制：已实现\n• P0-2 权限系统：已实现"

	// Simulate the policy-block path: finalText set, but NOT persisted
	// (mirrors applyFinalPolicyBlock when the response gate gives up retrying).
	a.run.stopReason = policy.StopCompleted
	a.run.finalText = finalText
	a.run.finalPersisted = false

	msgCountBefore := a.deps.ctxMgr.Len()
	result := a.loopRunner.finishRun(nil)

	if result.FinalOutput != finalText {
		t.Fatalf("FinalOutput = %q, want %q", result.FinalOutput, finalText)
	}
	if a.deps.ctxMgr.Len() != msgCountBefore+1 {
		t.Fatalf("expected final answer persisted to context, msgs %d → %d", msgCountBefore, a.deps.ctxMgr.Len())
	}
	last := lastAssistantContent(a.deps.ctxMgr.Build())
	if last != finalText {
		t.Fatalf("persisted assistant content = %q, want %q", last, finalText)
	}
}

// TestFinishRunDoesNotDoublePersistRecordableText ensures that when the final
// text was already recorded by a normal chat turn (recordable), finishRun does
// not append a duplicate assistant message.
func TestFinishRunDoesNotDoublePersistRecordableText(t *testing.T) {
	ctxMgr := ctxmgr.New(ctxmgr.Config{SystemPrompt: "test", ContextWindow: 128000})
	reg := tools.New()
	a := New(context.Background(), Config{Context: ctxMgr, Tools: reg})

	finalText := "all done"
	a.run.stopReason = policy.StopCompleted
	a.run.finalText = finalText
	a.run.finalPersisted = true // recordReasoningText already persisted it

	msgCountBefore := a.deps.ctxMgr.Len()
	result := a.loopRunner.finishRun(nil)

	if result.FinalOutput != finalText {
		t.Fatalf("FinalOutput = %q, want %q", result.FinalOutput, finalText)
	}
	if a.deps.ctxMgr.Len() != msgCountBefore {
		t.Fatalf("expected no duplicate persist, msgs %d → %d", msgCountBefore, a.deps.ctxMgr.Len())
	}
}

func TestFinishRunDoesNotPersistNonRecordableText(t *testing.T) {
	ctxMgr := ctxmgr.New(ctxmgr.Config{SystemPrompt: "test", ContextWindow: 128000})
	reg := tools.New()
	a := New(context.Background(), Config{Context: ctxMgr, Tools: reg})

	a.turnRunner.recordReasoningText(&reasoningResult{
		Action:      actionChat,
		ActionInput: "draft answer from hook-retried turn",
	}, true)
	a.turnRunner.recordReasoningText(&reasoningResult{
		Action:      actionChat,
		ActionInput: "LLM call failed: timeout",
		IsError:     true,
	}, false)
	a.run.stopReason = policy.StopCompleted

	msgCountBefore := a.deps.ctxMgr.Len()
	result := a.loopRunner.finishRun(nil)

	if result.FinalOutput != "LLM call failed: timeout" {
		t.Fatalf("FinalOutput = %q, want latest non-recordable text", result.FinalOutput)
	}
	if a.deps.ctxMgr.Len() != msgCountBefore {
		t.Fatalf("expected latest fallback to stay out of context, msgs %d → %d", msgCountBefore, a.deps.ctxMgr.Len())
	}
	last := lastAssistantContent(a.deps.ctxMgr.Build())
	if last != "draft answer from hook-retried turn" {
		t.Fatalf("persisted assistant content = %q, want previous recordable text", last)
	}
}

func TestInterruptedRunRestoresContextBeforeUserInput(t *testing.T) {
	ctxMgr := ctxmgr.New(ctxmgr.Config{SystemPrompt: "test", ContextWindow: 128000})
	a := New(context.Background(), Config{Context: ctxMgr, Tools: tools.New()})

	// A completed earlier turn after a tool result used to be lost because the
	// rollback searched globally for the last tool message.
	ctxMgr.AddAssistant(types.Message{Content: "old tool", ToolCalls: []types.ToolCall{{
		ID: "old", Type: "function", Function: types.FunctionCall{Name: "read", Arguments: `{}`},
	}}})
	ctxMgr.AddToolResultsBatch([]ctxmgr.ToolResultMsg{{
		Message: types.Message{Content: "old result", ToolCallID: "old"}, ToolName: "read",
	}})
	ctxMgr.Add("user", "follow-up")
	ctxMgr.AddAssistant(types.Message{Content: "completed answer after old tool"})
	compacted := ctxMgr.Snapshot()
	compacted.Archive = "summary of the old tool turn"
	compacted.Messages = compacted.Messages[2:]
	ctxMgr.Restore(compacted)

	before := ctxMgr.Snapshot()
	a.loopRunner.startRun("long task")
	for i := 0; i < 40; i++ {
		id := "call-" + strconv.Itoa(i)
		ctxMgr.AddAssistant(types.Message{Content: "working", ToolCalls: []types.ToolCall{{
			ID: id, Type: "function", Function: types.FunctionCall{Name: "read", Arguments: `{}`},
		}}})
		ctxMgr.AddToolResultsBatch([]ctxmgr.ToolResultMsg{{
			Message: types.Message{Content: "result", ToolCallID: id}, ToolName: "read",
		}})
	}
	a.run.stopReason = policy.StopInterrupted
	result := a.loopRunner.finishRun(nil)
	a.loopRunner.restoreInterruptedRun(before, result)
	after := ctxMgr.Snapshot()

	if !result.Interrupted || result.FinalOutput != msgInterrupted {
		t.Fatalf("result = %+v, want interrupted", result)
	}
	if !sameConversation(after, before) {
		t.Fatalf("interruption retained current run: before=%+v after=%+v", before, after)
	}
	if containsMessage(after.Messages, "long task") || containsMessage(after.Transcript, "long task") {
		t.Fatalf("interrupted request remained in context or transcript: %+v", after)
	}
}

func containsMessage(messages []types.Message, content string) bool {
	for _, message := range messages {
		if message.Content == content {
			return true
		}
	}
	return false
}

func TestInterruptedRunBeforeFirstToolDropsInputAndPreservesPriorHistory(t *testing.T) {
	ctxMgr := ctxmgr.New(ctxmgr.Config{SystemPrompt: "test", ContextWindow: 128000})
	a := New(context.Background(), Config{Context: ctxMgr, Tools: tools.New()})
	ctxMgr.Add("user", "previous")
	ctxMgr.AddAssistant(types.Message{Content: "previous answer"})
	before := ctxMgr.Snapshot()
	a.loopRunner.startRun("current request")

	a.run.stopReason = policy.StopInterrupted
	result := a.loopRunner.finishRun(nil)
	a.loopRunner.restoreInterruptedRun(before, result)

	if after := ctxMgr.Snapshot(); !sameConversation(after, before) {
		t.Fatalf("interruption before first tool changed history: before=%+v after=%+v", before, after)
	}
}

func sameConversation(a, b ctxmgr.ManagerSnapshot) bool {
	return a.Archive == b.Archive &&
		reflect.DeepEqual(a.Messages, b.Messages) &&
		reflect.DeepEqual(a.Transcript, b.Transcript)
}

func lastAssistantContent(msgs []types.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "assistant" {
			return strings.TrimSpace(msgs[i].Content)
		}
	}
	return ""
}
