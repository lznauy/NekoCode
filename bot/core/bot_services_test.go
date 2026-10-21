package core

import (
	"nekocode/bot/policy"
	"nekocode/bot/policy/ledger"
	"nekocode/bot/provider/types"
	"reflect"
	"testing"

	"nekocode/bot/config"
)

func TestSandboxAuthorityIncludesWorkspaceChanges(t *testing.T) {
	current := config.Default.Clone()
	next := current.Clone()
	if sandboxAuthorityChanged(&current, next) {
		t.Fatal("identical configuration changed sandbox authority")
	}
	next.Workspaces = []config.WorkspaceConfig{{Path: "/tmp/data", Access: "read-write"}}
	if !sandboxAuthorityChanged(&current, next) {
		t.Fatal("workspace change did not change sandbox authority")
	}
}

func TestAgentRebuildRetainsSessionUsageWithoutCountingANewTurn(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	b, err := NewAt(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := b.Close(); err != nil {
			t.Error(err)
		}
	})
	b.ag.AddCompletionTokens(37)
	b.mu.Lock()
	b.rebuildAgentLocked()
	b.mu.Unlock()
	if got := b.Metrics(); got.CompletionTokens != 37 || got.TurnPrompt != 0 || got.TurnCompletion != 0 || got.TurnTotal != 0 {
		t.Fatalf("rebuilding the agent changed usage ownership: %+v", got)
	}
}

func TestApplyConfigurationPreservesSessionState(t *testing.T) {
	b := newPersistTestBot(t)
	b.ctxMgr.Add("user", "Loaded skill instructions already in this session")
	b.ext.MarkSkillLoaded("session-skill")
	audit := ledger.Snapshot{
		ModifiedFiles:  []string{"changed.go"},
		ToolErrors:     []ledger.ToolEvent{{Name: "shell", Error: "verification failed"}},
		ToolEventCount: 2,
	}
	b.policy.Restore(audit)
	b.policy.RecordTool(policy.ToolResult{Name: "read", Args: map[string]any{"path": "stale.go"}})
	audit.ToolEventCount++
	b.ag.AddCompletionTokens(37)
	b.ag.RecordLLMUsage(types.StreamUsage{PromptTokens: 8, CompletionTokens: 37})
	sessionID := b.CurrentSessionID()
	before := b.ctxMgr.Snapshot().Messages
	store := b.ag.Executor().ExecutionState().OutputStore
	for range 2 {
		if err := b.ApplyConfiguration(b.Configuration()); err != nil {
			t.Fatal(err)
		}
		if b.CurrentSessionID() != sessionID || !reflect.DeepEqual(b.ctxMgr.Snapshot().Messages, before) {
			t.Fatal("configuration reload changed the current conversation")
		}
		if !b.ext.Snapshot().LoadedSkills["session-skill"] {
			t.Error("configuration reload forgot loaded skill state")
		}
		if got := b.policy.Snapshot(); !reflect.DeepEqual(got, audit) {
			t.Errorf("configuration reload lost audit facts: %+v", got)
		}
		if got := b.Metrics(); got.CompletionTokens != 37 || got.TurnPrompt != 0 || got.TurnCompletion != 0 || got.TurnTotal != 0 {
			t.Errorf("configuration reload assigned historical usage to a new turn: %+v", got)
		}
		if b.ag.Executor().ExecutionState().OutputStore != store {
			t.Error("configuration reload replaced session output ownership")
		}
	}
	if err := b.saveSession(); err != nil {
		t.Fatal(err)
	}
	saved, err := b.sess.Load(sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.CompletionTokens != 37 || !reflect.DeepEqual(saved.LoadedSkills, []string{"session-skill"}) || !reflect.DeepEqual(saved.Ledger, audit) {
		t.Fatalf("persisted state: completion=%d skills=%v ledger=%+v", saved.CompletionTokens, saved.LoadedSkills, saved.Ledger)
	}
}
