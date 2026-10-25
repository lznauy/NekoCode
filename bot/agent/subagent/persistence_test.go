package subagent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/extension/tool"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/execution"
	"nekocode/bot/extension/tool/runtime/workspace"
	"nekocode/bot/provider/types"
	"nekocode/bot/session"
	"nekocode/protocol"
)

func TestResumeRestoresOnlyOwnArtifactReadAccess(t *testing.T) {
	parentStore := core.NewOutputStore(t.TempDir())
	owned := parentStore.Child("subagents").Child("saved")
	path, err := owned.PreserveOutput("original evidence")
	if err != nil {
		t.Fatal(err)
	}
	otherPath, err := parentStore.Child("subagents").Child("other").PreserveOutput("other evidence")
	if err != nil {
		t.Fatal(err)
	}
	registry := tools.New()
	registry.SetWorkspace(workspace.New(t.TempDir(), nil))
	state := execution.NewExecutionState()
	state.OutputStore = parentStore
	snapshot := ctxmgr.ManagerSnapshot{Messages: []types.Message{
		{Role: "tool", ToolOutput: protocol.ToolOutputArtifact{Path: path}},
		{Role: "tool", ToolOutput: protocol.ToolOutputArtifact{Path: otherPath}},
	}}
	_, cleanup := New(Config{Tools: registry}).newExecutor(RunConfig{SubagentID: "saved", ToolState: state, ResumeContext: &snapshot})
	defer cleanup()
	if _, _, allowed, err := registry.Workspace().CheckRead(path); err != nil || !allowed {
		t.Fatalf("own artifact unreadable: %v", err)
	}
	if _, _, allowed, err := registry.Workspace().CheckWrite(path); err != nil || allowed {
		t.Fatal("own artifact granted write access")
	}
	if _, _, allowed, err := registry.Workspace().CheckRead(otherPath); err != nil || allowed {
		t.Fatal("another task's artifact granted read access")
	}
}

func completionScript() []types.StreamToken {
	return []types.StreamToken{{ToolCallDelta: &types.ToolCallDelta{Index: 0, ID: "submitted", Name: submitResultToolName,
		Arguments: `{"summary":"done","evidence":[],"files":[],"verification":"checked","unfinished":[],"risks":[]}`}}}
}

func TestLiveOutputSequenceMatchesResumedTranscript(t *testing.T) {
	script := append([]types.StreamToken{{ReasoningContent: "inspect evidence"}, {Content: "verified"}}, completionScript()...)
	llm := &scriptedLLM{scripts: [][]types.StreamToken{script}}
	resume := ctxmgr.ManagerSnapshot{TranscriptBase: 10, TranscriptLazy: true}
	var events []OutputEvent
	var committedSequence int
	_, err := New(Config{LLM: llm, Tools: tools.New()}).Run(t.Context(), RunConfig{
		Prompt: "check", Profile: Profile{Name: "explore"}, ResumeContext: &resume,
		OnOutput: func(ev OutputEvent) { events = append(events, ev) },
		SaveContext: func(snap ctxmgr.ManagerSnapshot, _, _ string, _ AttemptUsage) error {
			for i, msg := range snap.Transcript {
				if msg.Role == "assistant" {
					committedSequence = snap.TranscriptBase + i + 1
				}
			}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if committedSequence != 12 || len(events) != 4 {
		t.Fatalf("sequence=%d events=%+v", committedSequence, events)
	}
	for _, ev := range events {
		if ev.MessageSeq != committedSequence {
			t.Fatalf("live output is not aligned with committed history: %+v", ev)
		}
	}
	final := events[len(events)-1]
	if final.Action != protocol.StepActionSubAgentMessage || final.Text != "verified" || final.Reasoning != "inspect evidence" {
		t.Fatalf("incomplete final output: %+v", final)
	}
}

func TestDurableTaskCanResumeWithPreviousContext(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	profile := Profile{Name: "explore", SystemPrompt: "inspect"}
	run := func(resume bool, prompt string) *scriptedLLM {
		t.Helper()
		child, err := session.OpenSubagent("parent", "child", "/workspace", resume)
		if err != nil {
			t.Fatal(err)
		}
		defer child.Close()
		cfg := RunConfig{Prompt: prompt, Profile: profile}
		if resume {
			snap := child.Snapshot().ContextSnapshot()
			cfg.ResumeContext = &snap
		}
		if err := child.Begin(profile.Name, prompt, nil); err != nil {
			t.Fatal(err)
		}
		cfg.SaveContext = func(snapshot ctxmgr.ManagerSnapshot, status, result string, usage AttemptUsage) error {
			return child.Checkpoint(snapshot, status, result, session.SubagentAttemptUsage{
				TotalTokens: usage.TotalTokens, CacheHitTokens: usage.CacheHitTokens, CacheMissTokens: usage.CacheMissTokens,
				ToolUseCount: usage.ToolUseCount, DurationMs: usage.DurationMs, ContextTokens: usage.ContextTokens, ContextWindow: usage.ContextWindow,
			})
		}
		llm := &scriptedLLM{scripts: [][]types.StreamToken{completionScript()}}
		result, err := New(Config{LLM: llm, Tools: tools.New()}).Run(context.Background(), cfg)
		if err != nil || result.Status != StatusCompleted {
			t.Fatalf("run: %v, %v", result, err)
		}
		return llm
	}
	run(false, "first investigation")
	llm := run(true, "verify the fix")
	if !messagesContainText(llm.messages[0], "first investigation") || !messagesContainText(llm.messages[0], "verify the fix") {
		t.Fatal("resumed model request lost previous or current task")
	}
	meta, err := session.ReadSubagentMetadata("parent", "child")
	if err != nil {
		t.Fatal(err)
	}
	if meta.Status != "completed" || meta.RunCount != 2 || meta.Result == "" {
		t.Fatalf("metadata: %+v", meta)
	}
	path := filepath.Join(os.Getenv("HOME"), ".nekocode", "sessions", "parent", "subagents", "child", "transcript.jsonl")
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

func TestResumeClosesUnknownToolOutcomeWithoutReplaying(t *testing.T) {
	llm := &scriptedLLM{scripts: [][]types.StreamToken{completionScript()}}
	snap := ctxmgr.ManagerSnapshot{Messages: []types.Message{{Role: "assistant", ToolCalls: []types.ToolCall{{ID: "unfinished", Function: types.FunctionCall{Name: "shell", Arguments: `{"command":"dangerous"}`}}}}}}
	_, err := New(Config{LLM: llm, Tools: tools.New()}).Run(context.Background(), RunConfig{Prompt: "inspect current state", Profile: Profile{Name: "explore"}, ResumeContext: &snap})
	if err != nil {
		t.Fatal(err)
	}
	for _, msg := range llm.messages[0] {
		if msg.ToolCallID == "unfinished" && msg.IsError {
			return
		}
	}
	t.Fatal("missing explicit unknown-outcome tool result")
}

func TestCheckpointFailurePreventsModelExecution(t *testing.T) {
	want := errors.New("disk full")
	llm := &scriptedLLM{}
	_, err := New(Config{LLM: llm, Tools: tools.New()}).Run(context.Background(), RunConfig{Profile: Profile{Name: "explore"}, SaveContext: func(ctxmgr.ManagerSnapshot, string, string, AttemptUsage) error { return want }})
	if !errors.Is(err, want) || llm.calls != 0 {
		t.Fatalf("err=%v calls=%d", err, llm.calls)
	}
}

func TestCheckpointFailureAfterModelPreventsToolExecution(t *testing.T) {
	want := errors.New("disk full after response")
	llm := &scriptedLLM{scripts: [][]types.StreamToken{{{ToolCallDelta: &types.ToolCallDelta{Index: 0, ID: "call", Name: "shell", Arguments: `{}`}}}}}
	saves, toolsStarted := 0, 0
	_, err := New(Config{LLM: llm, Tools: tools.New()}).Run(context.Background(), RunConfig{
		Profile:    Profile{Name: "explore", Tools: []string{"shell"}},
		OnToolCall: func(ToolCallEvent) { toolsStarted++ },
		SaveContext: func(ctxmgr.ManagerSnapshot, string, string, AttemptUsage) error {
			saves++
			if saves >= 2 {
				return want
			}
			return nil
		},
	})
	if !errors.Is(err, want) || toolsStarted != 0 {
		t.Fatalf("err=%v tools started=%d", err, toolsStarted)
	}
}

func TestStartedFollowsInitialCheckpoint(t *testing.T) {
	var events []string
	llm := &scriptedLLM{scripts: [][]types.StreamToken{completionScript()}}
	_, err := New(Config{LLM: llm, Tools: tools.New()}).Run(context.Background(), RunConfig{
		Profile: Profile{Name: "explore"},
		SaveContext: func(_ ctxmgr.ManagerSnapshot, status, _ string, _ AttemptUsage) error {
			events = append(events, "save:"+status)
			return nil
		},
		OnStarted: func() { events = append(events, "started") },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) < 2 || events[0] != "save:running" || events[1] != "started" {
		t.Fatalf("lifecycle order = %v", events)
	}
}

func TestCheckpointIncludesContextUsage(t *testing.T) {
	var usage AttemptUsage
	llm := &scriptedLLM{scripts: [][]types.StreamToken{completionScript()}}
	_, err := New(Config{LLM: llm, Tools: tools.New()}).Run(context.Background(), RunConfig{
		Prompt: "inspect", Profile: Profile{Name: "explore"}, ContextWindow: 1000,
		SaveContext: func(_ ctxmgr.ManagerSnapshot, status, _ string, current AttemptUsage) error {
			if status == "completed" {
				usage = current
			}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if usage.ContextWindow != 1000 || usage.ContextTokens <= 0 {
		t.Fatalf("context usage missing: %+v", usage)
	}
}

func TestCancelledRunPersistsInterruptedStatus(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	lastStatus := ""
	_, err := New(Config{LLM: &scriptedLLM{}, Tools: tools.New()}).Run(ctx, RunConfig{
		Profile: Profile{Name: "explore"},
		SaveContext: func(_ ctxmgr.ManagerSnapshot, status, _ string, _ AttemptUsage) error {
			lastStatus = status
			return nil
		},
	})
	if !errors.Is(err, context.Canceled) || lastStatus != "interrupted" {
		t.Fatalf("err=%v status=%s", err, lastStatus)
	}
}
