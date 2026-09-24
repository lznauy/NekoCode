package contextmgr

import (
	"context"
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

func TestPruneStaleToolResultsReplacesLowRelevance(t *testing.T) {
	pruner := &fakePruner{ok: true, pruned: []bool{true}}
	m := &Manager{toolPruner: pruner}
	compressor := newReplacementCompactor(nil, 80)
	history := prunerTestHistory()

	out := m.pruneStaleToolResults(context.Background(), history, 100000, compressor)

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

	out := m.pruneStaleToolResults(context.Background(), history, 100000, compressor)

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
	out := m.pruneStaleToolResults(context.Background(), history, 100000, compressor)
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

	out := m.pruneStaleToolResults(context.Background(), history, 100000, compressor)

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
	m.pruneStaleToolResults(context.Background(), prunerTestHistory(), 100000, compressor)

	if !strings.Contains(pruner.goal, "Recent tool results:") {
		t.Fatalf("goal should carry recent tool-result context: %q", pruner.goal)
	}
	if !strings.Contains(pruner.goal, "recent file content") {
		t.Fatalf("goal should include the most recent tool result: %q", pruner.goal)
	}
}
