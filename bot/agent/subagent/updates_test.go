package subagent

import (
	"errors"
	ctxmgr "nekocode/bot/contextmgr"
	tools "nekocode/bot/extension/tool"
	"nekocode/bot/provider/types"
	"nekocode/protocol"
	"strings"
	"testing"
)

func TestPendingUpdatesReachModelBeforeRequest(t *testing.T) {
	model := &scriptedLLM{scripts: [][]types.StreamToken{completionScript()}}
	engine := New(Config{LLM: model, Tools: tools.New()})
	calls := 0
	_, err := engine.Run(t.Context(), RunConfig{Prompt: "original", Profile: Profile{Name: "explore"}, PendingUpdates: func(apply func([]protocol.BackgroundUpdate) error) error {
		calls++
		return apply([]protocol.BackgroundUpdate{{Sequence: 1, Text: "user-update-sentinel"}})
	}})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, msg := range model.messages[0] {
		if msg.Role == "user" && strings.Contains(msg.Content, "user-update-sentinel") {
			found = true
		}
	}
	if !found || calls != 1 {
		t.Fatalf("update missing or repeated: found=%v calls=%d", found, calls)
	}
}

func TestPendingUpdateErrorStopsBeforeModel(t *testing.T) {
	model := &scriptedLLM{}
	engine := New(Config{LLM: model, Tools: tools.New()})
	sentinel := errors.New("update storage failed")
	_, err := engine.Run(t.Context(), RunConfig{Prompt: "original", Profile: Profile{Name: "explore"}, PendingUpdates: func(func([]protocol.BackgroundUpdate) error) error { return sentinel }})
	if !errors.Is(err, sentinel) || model.calls != 0 {
		t.Fatalf("err=%v calls=%d", err, model.calls)
	}
}

func TestTaskUpdateReceiptSurvivesCheckpointAndResume(t *testing.T) {
	model := &scriptedLLM{scripts: [][]types.StreamToken{completionScript(), completionScript()}}
	engine := New(Config{LLM: model, Tools: tools.New()})
	var saved ctxmgr.ManagerSnapshot
	cfg := RunConfig{Prompt: "original", Profile: Profile{Name: "explore"}, PendingUpdates: func(apply func([]protocol.BackgroundUpdate) error) error {
		return apply([]protocol.BackgroundUpdate{{Sequence: 1, Text: "persisted-update-sentinel"}})
	}, SaveContext: func(snap ctxmgr.ManagerSnapshot, _ string, _ string, _ AttemptUsage) error { saved = snap; return nil }}
	if _, err := engine.Run(t.Context(), cfg); err != nil {
		t.Fatal(err)
	}
	if saved.TaskUpdateSequence != 1 {
		t.Fatalf("receipt not checkpointed: %d", saved.TaskUpdateSequence)
	}
	// Simulate a persisted transcript tail whose manifest receipt was not committed.
	saved.TaskUpdateSequence = 0
	// The runtime sidecar did not confirm consumption before the process stopped.
	cfg.ResumeContext = &saved
	if _, err := engine.Run(t.Context(), cfg); err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, msg := range saved.Messages {
		if msg.Role == "user" && msg.Content == "persisted-update-sentinel" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("duplicate update after resume: %d", count)
	}
}

// Keep a real engine-level counterexample to the tempting but invalid probe
// that sends two different tasks' updates into one synthetic Manager.
func TestDistinctTasksKeepIndependentUpdateReceipts(t *testing.T) {
	model := &scriptedLLM{scripts: [][]types.StreamToken{completionScript(), completionScript()}}
	engine := New(Config{LLM: model, Tools: tools.New()})
	for _, task := range []string{"task-A", "task-B"} {
		_, err := engine.Run(t.Context(), RunConfig{SubagentID: task, Prompt: "inspect", Profile: Profile{Name: "executor"}, PendingUpdates: func(apply func([]protocol.BackgroundUpdate) error) error {
			return apply([]protocol.BackgroundUpdate{{Sequence: 1, Text: task + "-update"}})
		}})
		if err != nil {
			t.Fatal(err)
		}
	}
	for i, task := range []string{"task-A", "task-B"} {
		found := false
		for _, msg := range model.messages[i] {
			if strings.Contains(msg.Content, task+"-update") {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s update missing", task)
		}
	}
}
