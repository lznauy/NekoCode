package contextmgr

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"nekocode/bot/decision"
	"nekocode/bot/provider/types"
)

type fakePruner struct {
	pruned     []bool
	candidates []decision.Candidate
	goal       string
	ok         bool
	calls      int
}

func (f *fakePruner) PruneTools(_ context.Context, goal string, candidates []decision.Candidate) ([]bool, bool) {
	f.calls++
	f.goal = goal
	f.candidates = candidates
	return f.pruned, f.ok
}

func prunerTestHistory() []types.Message {
	return []types.Message{
		{Role: "user", Content: "first task"},
		{Role: "assistant", ToolCalls: []types.ToolCall{{ID: "t1", Function: types.FunctionCall{Name: "shell", Arguments: "{}"}}}},
		{Role: "tool", ToolCallID: "t1", Content: "build log line 1\nbuild log line 2\nbuild log line 3"},
		{Role: "assistant", Content: "done with first task"},
		{Role: "user", Content: "second task"},
		{Role: "assistant", ToolCalls: []types.ToolCall{{ID: "t2", Function: types.FunctionCall{Name: "read", Arguments: "{}"}}}},
		{Role: "tool", ToolCallID: "t2", Content: "recent file content"},
		{Role: "assistant", Content: "recent answer"},
		{Role: "user", Content: "third task"},
		{Role: "assistant", Content: "third answer"},
		{Role: "user", Content: "fourth task"},
		{Role: "assistant", Content: "fourth answer"},
	}
}

func pruneTestPrefix(m *Manager, history []types.Message, compressor *replacementCompactor) []types.Message {
	cut := compressor.recentStart(history, 100000)
	prefix := m.pruneStaleToolResults(context.Background(), history, cut)
	out := append([]types.Message(nil), prefix...)
	return append(out, history[cut:]...)
}

func TestPruneStaleToolResultsReplacesLowRelevance(t *testing.T) {
	pruner := &fakePruner{ok: true, pruned: []bool{true}}
	m := &Manager{toolPruner: pruner}
	compressor := newReplacementCompactor(nil, 80)
	history := prunerTestHistory()

	out := pruneTestPrefix(m, history, compressor)

	// The old t1 result is pruned to a placeholder; the tool call record and
	// the recent t2 result survive verbatim.
	if !strings.Contains(out[2].Content, "pruned stale tool result") {
		t.Fatalf("old tool result not pruned: %q", out[2].Content)
	}
	if out[6].Content != "recent file content" {
		t.Fatalf("recent tool result must be untouched: %q", out[6].Content)
	}
	if out[1].ToolCalls[0].Function.Name != "shell" {
		t.Fatalf("tool call record must survive pruning: %+v", out[1])
	}
	if pruner.candidates[0].Tool != "shell" || !strings.Contains(pruner.candidates[0].Head, "build log line 1") {
		t.Fatalf("unexpected candidate: %+v", pruner.candidates[0])
	}
	if !strings.Contains(pruner.goal, "fourth task") {
		t.Fatalf("goal should be the most recent user turn: %q", pruner.goal)
	}
}

func TestPruneStaleToolResultsFailsOpen(t *testing.T) {
	pruner := &fakePruner{ok: false, pruned: []bool{true}}
	m := &Manager{toolPruner: pruner}
	compressor := newReplacementCompactor(nil, 80)
	history := prunerTestHistory()

	out := pruneTestPrefix(m, history, compressor)

	for i, msg := range out {
		if msg.Content != history[i].Content {
			t.Fatalf("fail-open must keep original content at %d", i)
		}
	}
	if pruner.calls != 1 {
		t.Fatalf("pruner calls = %d", pruner.calls)
	}
}

func TestPruneStaleToolResultsNilPrunerIsNoop(t *testing.T) {
	m := &Manager{}
	compressor := newReplacementCompactor(nil, 80)
	history := prunerTestHistory()
	out := pruneTestPrefix(m, history, compressor)
	if len(out) != len(history) {
		t.Fatal("nil pruner must not touch history")
	}
}

func TestPruneStaleAssistantMessages(t *testing.T) {
	long := strings.Repeat("analysis of the failure ", 20) // > minAssistantPruneRunes
	history := []types.Message{
		{Role: "user", Content: "first task"},
		{Role: "assistant", Content: long},
		{Role: "assistant", Content: "short reply"}, // below threshold: never a candidate
		{Role: "assistant", ToolCalls: []types.ToolCall{{ID: "t1", Function: types.FunctionCall{Name: "shell", Arguments: "{}"}}}},
		{Role: "tool", ToolCallID: "t1", Content: "result"},
		{Role: "user", Content: "second task"},
		{Role: "assistant", Content: "recent answer"},
		{Role: "user", Content: "third task"},
		{Role: "assistant", Content: "third answer"},
		{Role: "user", Content: "fourth task"},
	}
	pruner := &fakePruner{ok: true, pruned: []bool{true, true}}
	m := &Manager{toolPruner: pruner}
	compressor := newReplacementCompactor(nil, 80)

	out := pruneTestPrefix(m, history, compressor)

	if len(pruner.candidates) != 2 || pruner.candidates[0].Tool != assistantCandidate || pruner.candidates[1].Tool != "shell" {
		t.Fatalf("expected assistant message + old tool result as candidates: %+v", pruner.candidates)
	}
	if !strings.Contains(out[1].Content, "pruned stale assistant message") {
		t.Fatalf("long assistant message not pruned: %q", out[1].Content)
	}
	if !strings.Contains(out[4].Content, "pruned stale tool result") {
		t.Fatalf("old tool result not pruned: %q", out[4].Content)
	}
	if out[2].Content != "short reply" {
		t.Fatal("short messages must be untouched")
	}
}

func TestGoalIncludesRecentToolResults(t *testing.T) {
	pruner := &fakePruner{ok: true, pruned: []bool{true}}
	m := &Manager{toolPruner: pruner}
	compressor := newReplacementCompactor(nil, 80)
	pruneTestPrefix(m, prunerTestHistory(), compressor)

	if !strings.Contains(pruner.goal, "Recent tool results:") {
		t.Fatalf("goal should carry recent tool-result context: %q", pruner.goal)
	}
	if !strings.Contains(pruner.goal, "recent file content") {
		t.Fatalf("goal should include the most recent tool result: %q", pruner.goal)
	}
}

func TestPruningDoesNotChangeRecentRetentionBoundary(t *testing.T) {
	pruner := &fakePruner{ok: true}
	m := New(Config{ContextWindow: 12000, ToolPruner: pruner, Summarizer: func([]types.Message, string) (string, error) { return "<summary>archived</summary>", nil }})
	m.Add("user", "review task")
	for i := range 12 {
		id := fmt.Sprintf("call-%d", i)
		m.AddAssistant(types.Message{ToolCalls: []types.ToolCall{{ID: id, Function: types.FunctionCall{Name: "read", Arguments: "{}"}}}})
		m.AddToolResultsBatch([]ToolResultMsg{{ToolName: "read", Message: types.Message{ToolCallID: id, Content: strings.Repeat("a", 2000)}}})
	}
	before, compressor := m.compactionInput()
	cut := compressor.recentStart(before.history, before.budget)
	for _, msg := range before.history[:cut] {
		if msg.Role == "tool" {
			pruner.pruned = append(pruner.pruned, true)
		}
	}
	ok, err := m.Summarize(t.Context())
	if !ok || err != nil {
		t.Fatalf("summarize: %t %v", ok, err)
	}
	recent := m.Snapshot().Messages
	if len(recent) != len(before.history)-cut || recent[0].ToolCalls[0].ID != before.history[cut].ToolCalls[0].ID {
		t.Fatalf("recent changed after pruning: got %d want %d", len(recent), len(before.history)-cut)
	}
}
