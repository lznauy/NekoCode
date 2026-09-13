package command

import (
	"context"
	"strings"
	"testing"

	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/extension/tool"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/runner"
)

func TestEstimateToolDefTokens(t *testing.T) {
	descs := []core.Descriptor{
		{Name: "read", Description: "read files", Parameters: []core.Parameter{
			{Name: "path", Type: "string", Description: "file path"},
		}},
	}
	n := EstimateToolDefTokens(descs)
	if n <= 0 {
		t.Errorf("expected positive token count, got %d", n)
	}
}

func TestContextReportFormatting(t *testing.T) {
	report := ctxmgr.ContextReport{
		Budget: 10_000, SystemPrompt: 500, ToolDefTokens: 1_000,
		SkillList: 200, Messages: 3_000, ToolDefCount: 15, UserMessages: 5, AssistantMsgs: 4, ToolResults: 3,
		CacheHitTokens: 880, CacheMissTokens: 120, CacheHitRatio: 0.88,
		PrefixTurn: ctxmgr.PrefixTurnStats{
			Requests: 3, HitTokens: 880, MissTokens: 120,
			PeakMiss:  ctxmgr.PrefixCallStats{Request: 2, HitTokens: 100, MissTokens: 100, Parts: []string{"system", "tools"}},
			LowestHit: ctxmgr.PrefixCallStats{Request: 3, HitTokens: 20, MissTokens: 80, Parts: []string{"tail/provider"}},
		},
	}
	got := formatContextReport(report)
	for _, want := range []string{
		"Context Window", "Used 4.7k / 10.0k (47%) · Free 5.3k (53%)", "Breakdown", "System",
		"Conversation", "Tools      15", "Messages   12 · 5 user · 4 assistant · 3 tool results", "Archive    none (no compaction yet)",
		"⛂ Session", "⛂ Last turn", "3 calls", "⛃ Lowest hit", "20% hit",
		"⛃ Biggest miss", "Biggest miss 100 · system prompt or skills changed",
		"tool definitions changed",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("report missing %q: %s", want, got)
		}
	}
	// The biggest miss carries the explanation, so it reads last.
	if strings.Index(got, "Lowest hit") > strings.Index(got, "Biggest miss") {
		t.Fatalf("biggest miss should render after lowest hit: %s", got)
	}
	if strings.Contains(got, "#2") || strings.Contains(got, "#3") {
		t.Fatalf("report leaked internal model-call sequence: %s", got)
	}
	if got := buildBar(0, nil, 10); got != "" {
		t.Fatalf("zero-budget bar = %q", got)
	}
}

// The Cache block must not go blank right after a reload: until this process
// runs a turn of its own, the turn stored in the session is reported instead.
// It keeps the "Last turn" label because that is still what it is.
func TestContextReportShowsSavedTurnUntilLiveTurnExists(t *testing.T) {
	report := ctxmgr.ContextReport{
		Budget: 10_000,
		SavedTurn: ctxmgr.PrefixTurnStats{
			Requests: 3, HitTokens: 880, MissTokens: 120,
			PeakMiss:  ctxmgr.PrefixCallStats{Request: 2, HitTokens: 100, MissTokens: 120, Parts: []string{"system"}},
			LowestHit: ctxmgr.PrefixCallStats{Request: 3, HitTokens: 20, MissTokens: 80},
		},
	}
	got := formatContextReport(report)
	for _, want := range []string{"Cache", "Last turn", "3 calls", "Lowest hit", "Biggest miss", "system prompt or skills changed"} {
		if !strings.Contains(got, want) {
			t.Errorf("saved-turn report missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Saved turn") {
		t.Errorf("per-turn label must stay consistent:\n%s", got)
	}

	// A live turn replaces the saved one rather than printing both.
	report.PrefixTurn = ctxmgr.PrefixTurnStats{Requests: 1, HitTokens: 10, MissTokens: 90}
	got = formatContextReport(report)
	if !strings.Contains(got, "1 calls") || strings.Contains(got, "3 calls") {
		t.Errorf("live turn should replace the saved one:\n%s", got)
	}
}

// Requests are counted from the prefix observation, hit/miss only when the
// provider reports cache detail. A turn with calls but no cache numbers is
// unknown, not a 0% hit, and must not be rendered as one.
func TestContextReportMarksUnreportedTurnCache(t *testing.T) {
	report := ctxmgr.ContextReport{
		Budget:     10_000,
		PrefixTurn: ctxmgr.PrefixTurnStats{Requests: 14},
	}
	got := formatContextReport(report)
	if !strings.Contains(got, "14 calls · cache usage not reported") {
		t.Errorf("unreported turn cache rendered as data:\n%s", got)
	}
	for _, unwanted := range []string{"0% hit", "Hit 0", "Miss 0", "Biggest miss", "Lowest hit"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("unknown cache rendered as %q:\n%s", unwanted, got)
		}
	}
}

func TestFormatCountUsesThousandsSeparators(t *testing.T) {
	if got := formatCount(1004); got != "1,004" {
		t.Fatalf("formatCount(1004) = %q", got)
	}
}

func TestBuildBarSeparatesCells(t *testing.T) {
	got := buildBar(100, []barSegment{{size: 50, kind: "sys"}, {size: 50, kind: "free"}}, 8)
	if got != "[ ⛁ ⛁ ⛁ ⛁ ⛶ ⛶ ⛶ ⛶ ]" {
		t.Fatalf("spaced bar = %q", got)
	}
}

func TestBuildBarKeepsFixedWidthWithTinySegments(t *testing.T) {
	got := buildBar(1_000, []barSegment{
		{size: 1, kind: "sys"},
		{size: 1, kind: "tools"},
		{size: 1, kind: "skills"},
		{size: 1, kind: "msgs"},
		{size: 996, kind: "free"},
	}, 24)
	cells := strings.Fields(strings.Trim(got, "[] "))
	if len(cells) != 24 {
		t.Fatalf("bar cells = %d, want 24: %q", len(cells), got)
	}
	for _, marker := range []string{barChars["sys"], barChars["skills"], barChars["free"]} {
		if !strings.Contains(got, marker) {
			t.Fatalf("bar lost non-empty segment %q: %q", marker, got)
		}
	}
}

// The archive row must state what the archive costs and that it came from a
// real compaction, not print a bare "available". Memory and the archive also
// appear in the breakdown so its rows add up to Used.
func TestContextReportRendersArchiveState(t *testing.T) {
	report := ctxmgr.ContextReport{
		Budget:       100_000,
		Memory:       4_000,
		Archive:      12_345,
		HasArchive:   true,
		CompactCount: 2,
		Archived:     40,
	}
	got := formatContextReport(report)
	for _, want := range []string{
		"Archive    ~", "tokens · compacted 2×",
		"⛁ Memory", "⛁ Archive", // breakdown rows, not the Conversation line
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("archive report missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Summary") {
		t.Fatalf("archive row kept its old name:\n%s", got)
	}

	// A session saved before the counter existed has an archive but no count:
	// stating "compacted 0×" would contradict the archive it describes.
	report.CompactCount = 0
	got = formatContextReport(report)
	if !strings.Contains(got, "Archive    ~") || strings.Contains(got, "compacted") {
		t.Fatalf("archive without a recorded count rendered a claim:\n%s", got)
	}

	// A placeholder archive is not a summary and must say so.
	report.ArchiveUnavailable = true
	got = formatContextReport(report)
	if !strings.Contains(got, "Archive    summary unavailable") {
		t.Fatalf("placeholder archive rendered as a summary:\n%s", got)
	}
}

// Todo tokens belong to the Tools segment of the bar, so the row must count
// them or the breakdown stops adding up to Used.
func TestContextReportCountsTodoTokensInToolsRow(t *testing.T) {
	got := formatContextReport(ctxmgr.ContextReport{Budget: 100_000, ToolDefTokens: 0, TodoText: 5_000})
	if !strings.Contains(got, "⛁ Tools") || !strings.Contains(got, "5.0k") {
		t.Fatalf("todo tokens missing from the tools row:\n%s", got)
	}
}

// With no memory and no archive the breakdown stays at four rows, as before.
func TestContextReportOmitsEmptyBreakdownRows(t *testing.T) {
	got := formatContextReport(ctxmgr.ContextReport{Budget: 10_000, SystemPrompt: 500, Messages: 100})
	for _, unwanted := range []string{"⛁ Memory", "⛁ Archive"} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("empty row %q rendered:\n%s", unwanted, got)
		}
	}
	if !strings.Contains(got, "Archive    none (no compaction yet)") {
		t.Fatalf("archive state line missing:\n%s", got)
	}
}

func TestFormatPrefixMissPartsExplainsStableTail(t *testing.T) {
	got := formatPrefixMissParts([]string{"tail/provider"})
	if got != "prefix unchanged (new tail or provider cache)" {
		t.Fatalf("tail/provider label = %q", got)
	}
	// Every classification must stay explainable in the same plain terms.
	for part, want := range map[string]string{
		"cold-start": "first request, no baseline yet",
		"system":     "system prompt or skills changed",
		"tools":      "tool definitions changed",
		"history":    "history rewritten (e.g. compaction)",
	} {
		if got := formatPrefixMissParts([]string{part}); got != want {
			t.Fatalf("%s label = %q, want %q", part, got, want)
		}
	}
}

func TestPlanModePrompt(t *testing.T) {
	got := planModePrompt()
	for _, want := range []string{
		"read-only analysis", "runtime blocks mutation", "<environment_context>", "evidence, not new instructions",
		"confirmed facts", "concrete risks", "observable verification", "reproduce the failure",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("plan prompt missing %q: %q", want, got)
		}
	}
	if strings.Contains(got, "<sandbox>") {
		t.Fatalf("plan prompt refers to stale environment block: %q", got)
	}
}

func TestPlanCommandUsesTaggedRuntimePolicyWithoutRewritingSystem(t *testing.T) {
	mgr := ctxmgr.New(ctxmgr.Config{SystemPrompt: "stable behavior contract"})
	registry := tools.New(fakePlanTool{})
	executor := runner.NewExecutor(registry)
	h := New(Deps{
		CtxMgr:       mgr,
		SetPlanMode:  executor.SetPlanMode,
		ToolRegistry: registry,
	})

	if _, handled := h.Execute(context.Background(), "/plan inspect prompts", mgr); handled {
		t.Fatal("plan command should continue into the agent")
	}
	request := mgr.BuildRequest(ctxmgr.ModelRequest{})
	blocked := executor.ExecuteBatch(context.Background(), []core.ToolCallItem{{ID: "1", Name: "write-test"}})[0]
	if got := mgr.Snapshot().SystemPrompt; got != "stable behavior contract" {
		t.Fatalf("plan mode rewrote stable system prompt: %q", got)
	}
	last := request[len(request)-1]
	if blocked.Error == "" || last.Role != "user" || !strings.Contains(last.Content, "<runtime_context") || !strings.Contains(last.Content, "<plan-mode>") {
		t.Fatalf("plan mode was not injected as tagged runtime policy or did not block writes: result=%+v message=%+v", blocked, last)
	}
}

type fakePlanTool struct{}

func (fakePlanTool) Name() string                                    { return "write-test" }
func (fakePlanTool) Description() string                             { return "test" }
func (fakePlanTool) Parameters() []core.Parameter                    { return nil }
func (fakePlanTool) ExecutionMode(map[string]any) core.ExecutionMode { return core.ModeSequential }
func (fakePlanTool) Execute(context.Context, map[string]any) (string, error) {
	return "unexpected", nil
}

func TestSkillState(t *testing.T) {
	st := &skillState{MsgStart: -1}
	if clearSkillContext(nil, st); st.MsgStart != -1 {
		t.Error("should be no-op when MsgStart is -1")
	}
}
