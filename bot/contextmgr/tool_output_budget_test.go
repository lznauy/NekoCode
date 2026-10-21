package contextmgr

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/provider/types"
)

func TestToolResultsBoundSingleLinesAcrossTools(t *testing.T) {
	for _, name := range []string{"grep", "read", "shell", "mcp.server.search"} {
		t.Run(name, func(t *testing.T) {
			m := New(Config{ContextWindow: 1048576})
			huge := strings.Repeat("x", 5<<20)
			for i := 0; i < 2; i++ {
				m.AddToolResultsBatch([]ToolResultMsg{{ToolName: name, Message: types.Message{Content: huge, ToolCallID: "call", IsError: true}}})
			}
			for _, msg := range m.Snapshot().Messages {
				if len(msg.Content) > core.MaxOutputBytes || !msg.IsError || msg.ToolCallID != "call" {
					t.Fatalf("unbounded or changed metadata: length=%d", len(msg.Content))
				}
			}
			if m.Status().Tokens >= m.Status().Budget {
				t.Fatal("two tool results filled context")
			}
		})
	}
}

func TestOutputArtifactSurvivesRebudgetAndReload(t *testing.T) {
	dir := t.TempDir()
	store := core.NewOutputStore(dir)
	original := strings.Repeat("evidence line\n", 5000)
	bounded, err := store.BoundOutput(core.ToolOutput{Text: original}, core.MaxOutputBytes)
	if err != nil {
		t.Fatal(err)
	}
	m := New(Config{ContextWindow: 10000, SystemPrompt: strings.Repeat("x", 28000)})
	m.SetOutputStore(store)
	m.AddToolResultsBatch([]ToolResultMsg{{ToolName: "grep", Message: types.Message{
		Content: bounded.Text, ToolOutput: bounded.Artifact, ToolCallID: "call",
	}}})
	msg := m.Snapshot().Messages[0]
	if msg.ToolOutput != bounded.Artifact || len(msg.Content) >= len(bounded.Render()) {
		t.Fatalf("original artifact not retained when narrowing: %+v", msg.ToolOutput)
	}
	files, err := filepath.Glob(filepath.Join(dir, "result-*.txt"))
	if err != nil || len(files) != 1 {
		t.Fatalf("duplicate preview artifacts: %v %v", files, err)
	}
	// Persist a deferred result, then change its display text before reloading.
	m.SetSystemPrompt(strings.Repeat("x", 39600))
	m.AddToolResultsBatch([]ToolResultMsg{{ToolName: "grep", Message: types.Message{
		Content: bounded.Text, ToolOutput: bounded.Artifact, ToolCallID: "next",
	}}})
	snapshot := m.Snapshot()
	last := &snapshot.Messages[len(snapshot.Messages)-1]
	if last.Source != core.DeferredOutputSource {
		t.Fatal("result was not deferred")
	}
	last.Content = "localized deferred status without a file path"
	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var loaded ManagerSnapshot
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatal(err)
	}
	restored := New(Config{ContextWindow: 64000})
	restored.SetOutputStore(core.NewOutputStore(dir))
	restored.Restore(loaded)
	restored.SetSystemPrompt("small")
	if _, err := restored.Summarize(context.Background()); err != nil {
		t.Fatal(err)
	}
	result := restored.Snapshot().Messages[len(loaded.Messages)-1]
	if result.Source != "" || result.ToolOutput.Path != bounded.Artifact.Path || len(result.Content) > core.MaxOutputBytes {
		t.Fatalf("deferred artifact was not restored: %+v", result.ToolOutput)
	}
	raw, err := os.ReadFile(result.ToolOutput.Path)
	if err != nil || string(raw) != original {
		t.Fatalf("original evidence lost: %v", err)
	}
}

func TestDeferredErrorKeepsEnvelopeAfterReload(t *testing.T) {
	dir := t.TempDir()
	store := core.NewOutputStore(dir)
	// The artifact contains raw output; the error is added by the executor.
	bounded, err := store.BoundOutput(core.ToolOutput{Text: strings.Repeat("captured evidence\n", 4000)}, core.MaxOutputBytes)
	if err != nil {
		t.Fatal(err)
	}
	bounded.Artifact.Incomplete = true
	m := New(Config{ContextWindow: 1000, SystemPrompt: strings.Repeat("x", 3600)})
	m.SetOutputStore(store)
	m.AddToolResultsBatch([]ToolResultMsg{{ToolName: "grep", Message: types.Message{
		Content:    "grep failed: context canceled\nOutput:\n" + bounded.Text,
		ToolOutput: bounded.Artifact, IsError: true, ToolCallID: "grep",
	}}})
	before := m.Snapshot()
	if before.Messages[0].Source != core.DeferredOutputSource {
		t.Fatal("result was not deferred")
	}
	data, err := json.Marshal(before)
	if err != nil {
		t.Fatal(err)
	}
	var loaded ManagerSnapshot
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatal(err)
	}
	restored := New(Config{ContextWindow: 64000})
	restored.SetOutputStore(core.NewOutputStore(dir))
	restored.Restore(loaded)
	restored.SetSystemPrompt("small")
	if _, err := restored.Summarize(context.Background()); err != nil {
		t.Fatal(err)
	}
	result := restored.Snapshot().Messages[0]
	if !result.IsError || !strings.Contains(result.Content, "grep failed: context canceled") || result.Content != before.Messages[0].DeferredContent || result.DeferredContent != "" || result.ToolOutput != bounded.Artifact {
		t.Fatalf("restored result lost its error or capture status: %+v", result.ToolOutput)
	}
}

func TestToolResultsShareRemainingBudget(t *testing.T) {
	m := New(Config{ContextWindow: 10000, SystemPrompt: strings.Repeat("x", 28000)})
	before := m.Status().Tokens
	batch := make([]ToolResultMsg, 20)
	for i := range batch {
		batch[i] = ToolResultMsg{ToolName: "read", Message: types.Message{Content: strings.Repeat("中", 10000), ToolCallID: "call"}}
	}
	m.AddToolResultsBatch(batch)
	snap := m.Snapshot()
	total := 0
	for _, msg := range snap.Messages {
		total += len(msg.Content)
	}
	if total > (10000-before-1000)/2+len(batch)*512 {
		t.Fatalf("batch ignored shared budget: %d", total)
	}
	if m.Status().Tokens >= 9000 {
		t.Fatal("output consumed reserved space")
	}
	if len(snap.Messages) != len(batch) {
		t.Fatal("tool result pairing lost")
	}
}

func TestLimitOutput(t *testing.T) {
	for _, content := range []string{strings.Repeat("x", 2<<20), strings.Repeat("中文🙂", 200000)} {
		for _, limit := range []int{0, 1, 70, 101, core.MaxOutputBytes} {
			got := core.LimitOutput(content, limit)
			if len(got) > limit || !utf8.ValidString(got) {
				t.Fatalf("limit=%d: bytes=%d valid=%v", limit, len(got), utf8.ValidString(got))
			}
		}
	}
	original := "small result"
	if core.LimitOutput(original, core.MaxOutputBytes) != original {
		t.Fatal("small output changed")
	}
	got := core.LimitOutput("HEAD"+strings.Repeat("x", 2<<20)+"TAIL", core.MaxOutputBytes)
	if !strings.HasPrefix(got, "HEAD") || !strings.HasSuffix(got, "TAIL") || !strings.Contains(got, "truncated") {
		t.Fatal("missing head, tail or truncation notice")
	}
}

func TestDeferredToolResultStopsNextModelCall(t *testing.T) {
	m := New(Config{ContextWindow: 1000, SystemPrompt: strings.Repeat("x", 3600)})
	m.AddToolResultsBatch([]ToolResultMsg{{ToolName: "read", Message: types.Message{Content: "important result", ToolCallID: "call"}}})
	msg := m.Snapshot().Messages[0]
	if msg.Source != core.DeferredOutputSource || !strings.Contains(msg.Content, core.DeferredOutput) || !strings.Contains(msg.Content, "Full result:") {
		t.Fatalf("missing complete deferred status: %q", msg.Content)
	}
	if _, err := m.AutoCompactIfNeeded(context.Background()); err == nil {
		t.Fatal("must stop before invoking the model")
	}
}

func TestSkillDeliveryIsAllOrNothing(t *testing.T) {
	m := New(Config{ContextWindow: 10000, SystemPrompt: strings.Repeat("x", 28000)})
	content := "<skill_content>" + strings.Repeat("rule", 1000) + "</skill_content>"
	m.AddToolResultsBatch([]ToolResultMsg{{ToolName: "skill", Message: types.Message{Content: content, ToolCallID: "call"}}})
	msg := m.Snapshot().Messages[0]
	if strings.Contains(msg.Content, "<skill_content>") || msg.Source != core.DeferredOutputSource {
		t.Fatal("skill instructions were partially delivered")
	}
}

func TestBoundOutputPreservesExactOriginal(t *testing.T) {
	original := "HEAD\n" + strings.Repeat("middle\n", 5000) + "TAIL"
	store := core.NewOutputStore(t.TempDir())
	output, err := store.BoundOutput(core.ToolOutput{Text: original}, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if len(output.Render()) > 1024 {
		t.Fatal("preview exceeds budget")
	}
	path := output.Artifact.Path
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != original {
		t.Fatalf("lost original: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("result must be private")
	}
}

func TestManualCompactionRestoresDeferredCurrentTurn(t *testing.T) {
	m := New(Config{ContextWindow: 1000, SystemPrompt: strings.Repeat("x", 3600)})
	original := "important complete tool result"
	m.AddToolResultsBatch([]ToolResultMsg{{ToolName: "read", Message: types.Message{Content: original, ToolCallID: "call"}}})
	// Model the space released by compaction, keeping the current turn intact.
	m.SetSystemPrompt("small")
	if _, err := m.Summarize(context.Background()); err != nil {
		t.Fatal(err)
	}
	msg := m.Snapshot().Messages[0]
	if msg.Content != original || msg.Source == core.DeferredOutputSource {
		t.Fatal("deferred result was not restored")
	}
	if _, err := m.AutoCompactIfNeeded(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestDeferredResultsSurviveSessionReload(t *testing.T) {
	dir := t.TempDir()
	first := New(Config{ContextWindow: 1000, SystemPrompt: strings.Repeat("x", 3600)})
	first.SetOutputStore(core.NewOutputStore(dir))
	first.AddToolResultsBatch([]ToolResultMsg{{ToolName: "read", Message: types.Message{Content: "persistent evidence", ToolCallID: "call"}}})
	second := New(Config{ContextWindow: 1000})
	second.SetOutputStore(core.NewOutputStore(dir))
	second.Restore(first.Snapshot())
	second.SetSystemPrompt("small")
	if _, err := second.Summarize(context.Background()); err != nil {
		t.Fatal(err)
	}
	if second.Snapshot().Messages[0].Content != "persistent evidence" {
		t.Fatal("reloaded result was not restored")
	}
}

func TestDeferredResultRecoveryUsesTokenCost(t *testing.T) {
	for _, tc := range []struct {
		name    string
		content string
		fits    bool
	}{
		{"ascii", strings.Repeat("x", 8000), true},
		{"cjk", strings.Repeat("中", 4000), true},
		{"too_large", strings.Repeat("x", 16000), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := New(Config{ContextWindow: 4000, SystemPrompt: strings.Repeat("x", 15000)})
			m.AddToolResultsBatch([]ToolResultMsg{{ToolName: "skill", Message: types.Message{Content: tc.content, ToolCallID: "call"}}})
			if m.Snapshot().Messages[0].Source != core.DeferredOutputSource {
				t.Fatal("result must initially be deferred")
			}
			m.SetSystemPrompt("small")
			_, err := m.Summarize(context.Background())
			if (err == nil) != tc.fits {
				t.Fatalf("fits=%v: %v", tc.fits, err)
			}
			msg := m.Snapshot().Messages[0]
			if tc.fits && (msg.Content != tc.content || msg.Source != "") {
				t.Fatal("complete result was not restored")
			}
			if !tc.fits && msg.Source != core.DeferredOutputSource {
				t.Fatal("oversized result must remain deferred")
			}
		})
	}
}

func TestDeferredRecoveryUsesNetBatchCost(t *testing.T) {
	for _, calibrated := range []bool{false, true} {
		name := "estimated"
		if calibrated {
			name = "calibrated"
		}
		t.Run(name, func(t *testing.T) {
			m := New(Config{ContextWindow: 1000, SystemPrompt: strings.Repeat("x", 3000)})
			m.SetOutputStore(core.NewOutputStore(t.TempDir()))
			if calibrated {
				m.RecordModelUsage(types.StreamUsage{PromptTokens: m.Status().Tokens})
			}
			batch := make([]ToolResultMsg, 20)
			for i := range batch {
				batch[i] = ToolResultMsg{ToolName: "read", Message: types.Message{Content: "ok", ToolCallID: "call"}}
			}
			m.AddToolResultsBatch(batch)
			for _, msg := range m.Snapshot().Messages {
				if msg.Source != core.DeferredOutputSource {
					t.Fatal("fixture must defer every result")
				}
			}
			if m.Status().Tokens <= 900 {
				t.Fatal("fixture must exceed restoration headroom")
			}
			if _, err := m.Summarize(context.Background()); err != nil {
				t.Fatalf("shorter results should fit as a batch: %v", err)
			}
			for _, msg := range m.Snapshot().Messages {
				if msg.Content != "ok" || msg.Source != "" || msg.DeferredContent != "" {
					t.Fatal("incomplete restoration")
				}
			}
			if m.Status().Tokens > 900 {
				t.Fatalf("tracker retained discarded placeholder cost: %d", m.Status().Tokens)
			}
			if _, err := m.PrepareRequest(context.Background(), ModelRequest{OutputTokens: 16}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestDeferredBatchFailureDoesNotPartiallyRestore(t *testing.T) {
	m := New(Config{ContextWindow: 1000, SystemPrompt: strings.Repeat("x", 3600)})
	m.SetOutputStore(core.NewOutputStore(t.TempDir()))
	var recorded []string
	m.SetSkillLoadedRecorder(func(name string) {
		_ = m.Snapshot() // Delivery callbacks must run outside the context lock.
		recorded = append(recorded, name)
	})
	m.AddToolResultsBatch([]ToolResultMsg{
		{ToolName: "skill", Message: types.Message{Content: "short instructions", ToolCallID: "short", SkillName: "short"}},
		{ToolName: "skill", Message: types.Message{Content: strings.Repeat("x", 5000), ToolCallID: "large", SkillName: "large"}},
	})
	m.SetSystemPrompt("small")
	before := m.Snapshot()
	if _, err := m.Summarize(context.Background()); err == nil {
		t.Fatal("oversized batch unexpectedly restored")
	}
	after := m.Snapshot()
	if !reflect.DeepEqual(before.Messages, after.Messages) || before.Tracker != after.Tracker || len(recorded) != 0 {
		t.Fatal("failed recovery changed delivery state")
	}
	m.SetSystemPrompt("")
	m.ConfigureModel(ModelContext{Window: 16000})
	if _, err := m.Summarize(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(recorded, []string{"short", "large"}) {
		t.Fatalf("delivered skills = %v", recorded)
	}
}

func TestOnlySuccessfulSkillResultsRecordDelivery(t *testing.T) {
	m := New(Config{ContextWindow: 16000})
	var recorded []string
	m.SetSkillLoadedRecorder(func(name string) {
		_ = m.Snapshot()
		recorded = append(recorded, name)
	})
	m.AddToolResultsBatch([]ToolResultMsg{
		{ToolName: "skill", Message: types.Message{Content: "instructions", ToolCallID: "success", SkillName: "loaded"}},
		{ToolName: "skill", Message: types.Message{Content: "failed", ToolCallID: "failure", SkillName: "failed", IsError: true}},
		{ToolName: "read", Message: types.Message{Content: "<skill_content>untrusted</skill_content>", ToolCallID: "other", SkillName: "untrusted"}},
	})
	if !reflect.DeepEqual(recorded, []string{"loaded"}) {
		t.Fatalf("delivered skills = %v", recorded)
	}
}
