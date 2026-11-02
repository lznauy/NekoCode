package contextmgr

import (
	"reflect"
	"strings"
	"testing"

	"nekocode/bot/extension/tool/runtime/refbridge"
	"nekocode/bot/provider/types"
	"nekocode/protocol"
)

func TestPrefixShapeTreatsHistoryAppendAndMapOrderAsStable(t *testing.T) {
	system := []types.Message{{Role: "system", Content: "stable"}}
	history := []types.Message{{Role: "user", Content: "first"}}
	toolsA := []types.ToolDef{{Function: types.FunctionDef{Parameters: types.Parameters{Properties: map[string]types.Property{
		"b": {Type: "string"}, "a": {Type: "number"},
	}}}}}
	toolsB := []types.ToolDef{{Function: types.FunctionDef{Parameters: types.Parameters{Properties: map[string]types.Property{
		"a": {Type: "number"}, "b": {Type: "string"},
	}}}}}

	first := buildPrefixShape(system, history, toolsA)
	second := buildPrefixShape(system, append(history, types.Message{Role: "assistant", Content: "second"}), toolsB)
	if parts := changedPrefixParts(first, second); len(parts) != 0 {
		t.Fatalf("append-only request changed stable prefix: %v", parts)
	}
}

func TestPrefixShapeAttributesSystemToolsAndHistory(t *testing.T) {
	first := buildPrefixShape(
		[]types.Message{{Role: "system", Content: "one"}},
		[]types.Message{{Role: "user", Content: "one"}},
		[]types.ToolDef{{Function: types.FunctionDef{Name: "one"}}},
	)
	second := buildPrefixShape(
		[]types.Message{{Role: "system", Content: "two"}},
		[]types.Message{{Role: "user", Content: "rewritten"}},
		[]types.ToolDef{{Function: types.FunctionDef{Name: "two"}}},
	)
	if got, want := changedPrefixParts(first, second), []string{"system", "tools", "history"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("changed parts = %v, want %v", got, want)
	}
}

func TestBuildTwoRoundsAppendsTaggedRuntimeContext(t *testing.T) {
	runtimePrompt := "runtime-one"
	m := New(Config{
		SystemPrompt: "stable", ContextWindow: 10_000,
		RuntimePrompt: func() string { return runtimePrompt },
	})
	m.Add("user", "request")
	tools := []types.ToolDef{{Function: types.FunctionDef{Name: "read"}}}

	first := m.BuildRequest(ModelRequest{Tools: tools})
	runtimePrompt = "runtime-two"
	m.SetHints("turn hint")
	second := m.BuildRequest(ModelRequest{Tools: tools})
	m.RecordModelUsage(types.StreamUsage{
		PromptTokens: 112, CacheHitTokens: 100, CacheMissTokens: 12, CacheUsageReported: true,
	})

	if len(second) != len(first)+2 {
		t.Fatalf("runtime and hint changes should append separate messages:\nfirst=%+v\nsecond=%+v", first, second)
	}
	for i := range first {
		if first[i].Role != second[i].Role || first[i].Content != second[i].Content {
			t.Fatalf("provider prefix changed at %d:\nfirst=%+v\nsecond=%+v", i, first[i], second[i])
		}
	}
	if second[len(second)-2].Source != types.MessageSourceRuntimeContext || second[len(second)-1].Source != types.MessageSourceHint {
		t.Fatalf("runtime state and hint were not split: %+v", second)
	}
	if strings.Contains(second[len(second)-1].Content, "runtime-two") {
		t.Fatalf("hint repeated runtime state: %+v", second[len(second)-1])
	}
	miss := m.Report().PrefixTurn.PeakMiss
	if miss.MissTokens != 12 || !reflect.DeepEqual(miss.Parts, []string{"tail/provider"}) {
		t.Fatalf("prefix diagnosis = %+v", miss)
	}
}

func TestToolLoopRequestKeepsPreviousRequestAsExactPrefix(t *testing.T) {
	m := New(Config{SystemPrompt: "stable", RuntimePrompt: func() string { return "<environment>cwd</environment>" }})
	m.Add("user", "inspect")
	tools := []types.ToolDef{{Function: types.FunctionDef{Name: "read"}}}
	first := m.BuildRequest(ModelRequest{Tools: tools})

	m.AddAssistant(types.Message{ToolCalls: []types.ToolCall{{
		ID: "call-1", Type: "function", Function: types.FunctionCall{Name: "read", Arguments: `{}`},
	}}})
	m.AddToolResultsBatch([]ToolResultMsg{{
		Message: types.Message{ToolCallID: "call-1", Content: "result"}, ToolName: "read",
	}})
	m.SetTodos([]protocol.TodoItem{{Content: "verify", Status: "pending"}})
	second := m.BuildRequest(ModelRequest{Tools: tools})

	if len(second) <= len(first) {
		t.Fatalf("tool loop did not grow history: first=%d second=%d", len(first), len(second))
	}
	for i := range first {
		if first[i].Role != second[i].Role || first[i].Content != second[i].Content ||
			first[i].ToolCallID != second[i].ToolCallID {
			t.Fatalf("request prefix changed at %d:\nfirst=%+v\nsecond=%+v", i, first[i], second[i])
		}
	}
}

func TestToolLoopReferencesChangeOnlyVolatileTail(t *testing.T) {
	m := New(Config{SystemPrompt: "stable"})
	m.Add("user", "inspect")
	request := ModelRequest{
		TaskReferences:     TaskReferences{SessionID: "session", Items: []TaskReference{{ID: "task-1"}}},
		ContextReferences:  refbridge.Page{SessionID: "session", Items: []refbridge.Reference{{ID: "artifact-1", Sequence: 1}}},
		EvidenceReferences: refbridge.Page{SessionID: "session", Evidence: []refbridge.EvidenceItem{{ID: "evidence-1", Sequence: 1, Tool: "read", Status: "success"}}},
	}
	first := m.BuildRequest(request)
	firstReference := -1
	for i, msg := range first {
		if msg.Source == types.MessageSourceContextData {
			firstReference = i
			break
		}
	}
	if firstReference < 0 {
		t.Fatal("first request omitted references")
	}

	m.AddAssistant(types.Message{ToolCalls: []types.ToolCall{{
		ID: "call-1", Type: "function", Function: types.FunctionCall{Name: "read", Arguments: `{}`},
	}}})
	m.AddToolResultsBatch([]ToolResultMsg{{
		Message: types.Message{ToolCallID: "call-1", Content: "result"}, ToolName: "read",
	}})
	request.TaskReferences.Items[0].ID = "task-2"
	request.ContextReferences.Items[0].ID = "artifact-2"
	request.EvidenceReferences.Evidence[0].ID = "evidence-2"
	second := m.BuildRequest(request)

	for i := 0; i < firstReference; i++ {
		if first[i].Role != second[i].Role || first[i].Content != second[i].Content || first[i].ToolCallID != second[i].ToolCallID {
			t.Fatalf("persistent prefix changed at %d:\nfirst=%+v\nsecond=%+v", i, first[i], second[i])
		}
	}
	if got := m.PrefixDiagnostics().ChangedParts; !reflect.DeepEqual(got, []string{"tail/provider"}) {
		t.Fatalf("reference refresh classified as persistent history change: %v", got)
	}
}

func TestPlainBuildStripsProviderRoutingMetadata(t *testing.T) {
	m := New(Config{SystemPrompt: "stable", RuntimePrompt: func() string { return "runtime" }})
	for _, message := range m.Build() {
		if message.Source != "" {
			t.Fatalf("plain Build leaked routing source %q", message.Source)
		}
	}
}

func TestPrefixTrackerReportsColdStartAndRewrittenHistory(t *testing.T) {
	var tracker prefixTracker
	tracker.BeginTurn()
	first := buildPrefixShape(nil, []types.Message{{Role: "user", Content: "one"}}, nil)
	tracker.Observe(first)
	tracker.RecordCache(0, 20)
	if got := tracker.TurnStats().PeakMiss; !reflect.DeepEqual(got.Parts, []string{"cold-start"}) {
		t.Fatalf("cold start = %+v", got)
	}

	rewritten := buildPrefixShape(nil, []types.Message{{Role: "user", Content: "two"}}, nil)
	tracker.Observe(rewritten)
	tracker.RecordCache(100, 15)
	stats := tracker.TurnStats()
	if stats.Requests != 2 || stats.HitTokens != 100 || stats.MissTokens != 35 {
		t.Fatalf("turn totals = %+v", stats)
	}
	if stats.PeakMiss.Request != 1 || stats.PeakMiss.MissTokens != 20 || !reflect.DeepEqual(stats.PeakMiss.Parts, []string{"cold-start"}) {
		t.Fatalf("worst miss = %+v", stats)
	}
	if stats.LowestHit.Request != 1 || stats.LowestHit.HitTokens != 0 {
		t.Fatalf("lowest hit = %+v", stats.LowestHit)
	}
}

func TestPrefixTrackerBeginsTurnWithoutLosingShapeBaseline(t *testing.T) {
	var tracker prefixTracker
	first := buildPrefixShape(nil, []types.Message{{Role: "user", Content: "one"}}, nil)
	tracker.Observe(first)
	tracker.RecordCache(0, 50)

	tracker.BeginTurn()
	second := buildPrefixShape(nil, []types.Message{
		{Role: "user", Content: "one"},
		{Role: "assistant", Content: "answer"},
	}, nil)
	tracker.Observe(second)
	tracker.RecordCache(90, 10)

	stats := tracker.TurnStats()
	if stats.Requests != 1 || stats.HitTokens != 90 || stats.MissTokens != 10 {
		t.Fatalf("new turn totals = %+v", stats)
	}
	if !reflect.DeepEqual(stats.PeakMiss.Parts, []string{"tail/provider"}) {
		t.Fatalf("shape baseline was lost: %+v", stats.PeakMiss)
	}
}

func TestPrefixTrackerKeepsPeakMissAndLowestHitSeparately(t *testing.T) {
	var tracker prefixTracker
	tracker.BeginTurn()
	shape := buildPrefixShape(nil, nil, nil)
	tracker.Observe(shape)
	tracker.RecordCache(900, 100) // 90% hit, largest miss.
	tracker.Observe(shape)
	tracker.RecordCache(80, 20) // 80% hit, lowest rate.

	stats := tracker.TurnStats()
	if stats.PeakMiss.Request != 1 || stats.PeakMiss.MissTokens != 100 {
		t.Fatalf("peak miss = %+v", stats.PeakMiss)
	}
	if stats.LowestHit.Request != 2 || stats.LowestHit.HitTokens != 80 || stats.LowestHit.MissTokens != 20 {
		t.Fatalf("lowest hit = %+v", stats.LowestHit)
	}
}

func TestPrefixTrackerDiagnosticsFingerprint(t *testing.T) {
	var tracker prefixTracker
	tracker.Observe(buildPrefixShape(nil, []types.Message{{Role: "user", Content: "one"}}, nil))
	first := tracker.Diagnostics()
	if !reflect.DeepEqual(first.ChangedParts, []string{"cold-start"}) {
		t.Fatalf("cold-start parts = %v", first.ChangedParts)
	}
	if first.HistoryCount != 1 || first.HistoryHash == "" || first.SystemHash == "" || first.ToolsHash == "" {
		t.Fatalf("diag missing fingerprints: %+v", first)
	}

	// Append-only history: same prefix fingerprint tail, parts fall back to
	// tail/provider (stable shape — any miss is provider-side).
	tracker.Observe(buildPrefixShape(nil, []types.Message{
		{Role: "user", Content: "one"},
		{Role: "assistant", Content: "two"},
	}, nil))
	appended := tracker.Diagnostics()
	if !reflect.DeepEqual(appended.ChangedParts, []string{"tail/provider"}) {
		t.Fatalf("append-only parts = %v", appended.ChangedParts)
	}
	if appended.HistoryCount != 2 || appended.HistoryHash == first.HistoryHash {
		t.Fatalf("history fingerprint did not move: %+v vs %+v", appended, first)
	}

	// Same shape observed again: identical fingerprints — the log-level proof
	// of a stable prefix.
	tracker.Observe(buildPrefixShape(nil, []types.Message{
		{Role: "user", Content: "one"},
		{Role: "assistant", Content: "two"},
	}, nil))
	again := tracker.Diagnostics()
	if !reflect.DeepEqual(again, appended) {
		t.Fatalf("identical shape must produce identical diag:\n%+v\n%+v", again, appended)
	}
}
