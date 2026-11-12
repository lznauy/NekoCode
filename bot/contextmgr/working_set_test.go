package contextmgr

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/provider/types"
)

func appendWorkingSetBatch(t *testing.T, m *Manager, store *core.OutputStore, id, name string, isError bool) {
	t.Helper()
	output, err := store.BoundOutput(core.ToolOutput{Text: strings.Repeat(id+" evidence\n", 8000)}, core.MaxOutputBytes)
	if err != nil {
		t.Fatal(err)
	}
	m.AddAssistant(types.Message{Role: "assistant", ToolCalls: []types.ToolCall{{ID: id, Function: types.FunctionCall{Name: name, Arguments: `{}`}}}})
	m.AddToolResultsBatch([]ToolResultMsg{{ToolName: name, Message: types.Message{ToolCallID: id, Content: output.Text, ToolOutput: output.Artifact, IsError: isError, ReadID: "read-" + id}}})
}

func TestWorkingSetCheckpointShrinksOldArtifactsPreservingTraceAndRecentPairs(t *testing.T) {
	store := core.NewOutputStore(t.TempDir())
	m := New(Config{ContextWindow: 1_000_000})
	m.SetOutputStore(store)
	m.Add("user", "review code")
	for i := 0; i < 12; i++ {
		appendWorkingSetBatch(t, m, store, fmt.Sprint(i), "read", false)
	}
	m.ConfigureModel(ModelContext{Window: 60000, AutoCompactPercent: 80})
	before := m.Snapshot()
	request := ModelRequest{OutputTokens: 1000}
	projected, err := m.PrepareRequest(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	after := m.Snapshot()
	oldBytes, newBytes := 0, 0
	changed := 0
	for i, msg := range before.Messages {
		oldBytes += len(msg.Content)
		newBytes += len(after.Messages[i].Content)
		if msg.Content != after.Messages[i].Content {
			changed++
			if after.Messages[i].ReadID != "" {
				t.Fatal("shortened source preview retained edit authorization")
			}
			if msg.ToolOutput != after.Messages[i].ToolOutput {
				t.Fatal("artifact identity changed")
			}
			if _, err := os.ReadFile(after.Messages[i].ToolOutput.Path); err != nil {
				t.Fatal(err)
			}
		}
	}
	if changed == 0 || newBytes >= oldBytes {
		t.Fatalf("working context did not shrink: %d -> %d", oldBytes, newBytes)
	}
	if !reflect.DeepEqual(before.Transcript, after.Transcript) {
		t.Fatal("working-set checkpoint changed transcript")
	}
	// Each retained batch contains the call and matching result, unchanged.
	recent := workingSetRecentBatches * 2
	if !reflect.DeepEqual(before.Messages[len(before.Messages)-recent:], after.Messages[len(after.Messages)-recent:]) {
		t.Fatal("recent working context changed")
	}
	again, err := m.PrepareRequest(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(projected, again) {
		t.Fatal("unchanged request rewrote cached history again")
	}
	t.Logf("active message bytes: %d -> %d; archived results remain readable", oldBytes, newBytes)
}

func TestWorkingSetDoesNotShrinkErrorsExecutionLogsOrUnrecoverableResults(t *testing.T) {
	store := core.NewOutputStore(t.TempDir())
	m := New(Config{ContextWindow: 1_000_000})
	m.SetOutputStore(store)
	appendWorkingSetBatch(t, m, store, "failure", "grep", true)
	appendWorkingSetBatch(t, m, store, "shell", "shell", false)
	foreign := core.NewOutputStore(t.TempDir())
	appendWorkingSetBatch(t, m, foreign, "foreign", "read", false)
	appendWorkingSetBatch(t, m, store, "missing", "read", false)
	for i := 0; i < 12; i++ {
		appendWorkingSetBatch(t, m, store, fmt.Sprint(i), "grep", false)
	}
	before := m.Snapshot()
	if err := os.Remove(before.Messages[7].ToolOutput.Path); err != nil {
		t.Fatal(err)
	}
	m.checkpointWorkingSet()
	after := m.Snapshot()
	for _, i := range []int{1, 3, 5, 7} {
		if before.Messages[i].Content != after.Messages[i].Content {
			t.Fatalf("protected result %d shrank", i)
		}
	}
	if reflect.DeepEqual(before.Messages, after.Messages) {
		t.Fatal("fixture did not trigger checkpoint")
	}
}

func TestWorkingSetKeepsIncompleteParallelBatchesAndUnarchivedResults(t *testing.T) {
	store := core.NewOutputStore(t.TempDir())
	m := New(Config{ContextWindow: 1_000_000})
	m.SetOutputStore(store)
	output, err := store.BoundOutput(core.ToolOutput{Text: strings.Repeat("code\n", 8000)}, core.MaxOutputBytes)
	if err != nil {
		t.Fatal(err)
	}
	m.AddAssistant(types.Message{Role: "assistant", ToolCalls: []types.ToolCall{{ID: "a", Function: types.FunctionCall{Name: "read"}}, {ID: "b", Function: types.FunctionCall{Name: "read"}}}})
	m.AddToolResultsBatch([]ToolResultMsg{{ToolName: "read", Message: types.Message{ToolCallID: "a", Content: output.Text, ToolOutput: output.Artifact}}})
	m.AddAssistant(types.Message{Role: "assistant", ToolCalls: []types.ToolCall{{ID: "plain", Function: types.FunctionCall{Name: "read"}}}})
	m.AddToolResultsBatch([]ToolResultMsg{{ToolName: "read", Message: types.Message{ToolCallID: "plain", Content: strings.Repeat("plain\n", 1000)}}})
	for i := 0; i < 12; i++ {
		appendWorkingSetBatch(t, m, store, fmt.Sprint(i), "read", false)
	}
	before := m.Snapshot()
	m.checkpointWorkingSet()
	after := m.Snapshot()
	for _, i := range []int{1, 3} {
		if !reflect.DeepEqual(before.Messages[i], after.Messages[i]) {
			t.Fatal("incomplete or unrecoverable batch changed")
		}
	}
	// Saved reduced context can be restored, while the complete trace survives.
	reloaded := New(Config{})
	reloaded.SetOutputStore(store)
	reloaded.Restore(after)
	if !reflect.DeepEqual(reloaded.Snapshot().Transcript, after.Transcript) {
		t.Fatal("reload lost original trace")
	}
}

func TestWorkingSetDoesNotRewriteWarmHistoryWithoutPressure(t *testing.T) {
	store := core.NewOutputStore(t.TempDir())
	m := New(Config{ContextWindow: 1_000_000})
	m.SetOutputStore(store)
	for i := 0; i < 12; i++ {
		appendWorkingSetBatch(t, m, store, fmt.Sprint(i), "read", false)
	}
	before := m.Snapshot()
	if _, err := m.PrepareRequest(context.Background(), ModelRequest{OutputTokens: 1000}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before.Messages, m.Snapshot().Messages) {
		t.Fatal("working-set optimization broke a warm prefix without pressure")
	}
}

func TestArtifactCheckpointCanAvoidSummaryModelCall(t *testing.T) {
	store := core.NewOutputStore(t.TempDir())
	m := New(Config{ContextWindow: 1_000_000})
	m.SetOutputStore(store)
	for i := 0; i < 12; i++ {
		m.Add("user", fmt.Sprintf("inspect part %d", i))
		appendWorkingSetBatch(t, m, store, fmt.Sprint(i), "read", false)
	}
	m.ConfigureModel(ModelContext{Window: 60000, AutoCompactPercent: 80})
	summaries := 0
	m.state.compressor.summarizer = func([]types.Message, string) (string, error) {
		summaries++
		return "", fmt.Errorf("unexpected summary inference")
	}
	changed, err := m.AutoCompactIfNeeded(context.Background())
	if err != nil || !changed || summaries != 0 {
		t.Fatalf("artifact checkpoint did not avoid inference: changed=%v summaries=%d err=%v", changed, summaries, err)
	}
	if m.Snapshot().Archive != "" || m.Snapshot().CompactCount != 0 {
		t.Fatal("deterministic working-set change was mislabeled as model compaction")
	}
}

func TestWorkingSetPreservesProviderCalibrationAndUsage(t *testing.T) {
	store := core.NewOutputStore(t.TempDir())
	m := New(Config{ContextWindow: 1_000_000})
	m.SetOutputStore(store)
	for i := 0; i < 12; i++ {
		appendWorkingSetBatch(t, m, store, fmt.Sprint(i), "read", false)
	}
	// Provider framing/tokenization can cost more than the local estimator.
	// Narrowing visible results must not erase that known positive correction.
	const correction = 20000
	m.state.tracker.RecordPrompt(m.totalTokenEstimate() + correction)
	m.state.tracker.RecordCache(1234, 567)
	if !m.checkpointWorkingSet() {
		t.Fatal("fixture did not shrink")
	}
	if got := m.state.tracker.PromptEstimate() - m.totalTokenEstimate(); got != correction {
		t.Fatalf("provider calibration lost: correction=%d, want %d", got, correction)
	}
	if hit, miss := m.state.tracker.CacheStats(); hit != 1234 || miss != 567 {
		t.Fatalf("usage changed: hit=%d miss=%d", hit, miss)
	}
}
