package session

import (
	"bytes"
	"encoding/json"
	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/provider/types"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRejectedResumeDoesNotRepairHistory(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	child, err := OpenSubagent("parent", "child", "/project", false)
	if err != nil {
		t.Fatal(err)
	}
	child.Begin("explore", "inspect", nil)
	if err := child.Checkpoint(ctxmgr.ManagerSnapshot{}, "completed", "done"); err != nil {
		t.Fatal(err)
	}
	child.Close()
	root, _ := subagentRoot("parent")
	path := filepath.Join(root, "child", "transcript.jsonl")
	before := []byte(`{"incomplete":`)
	if err := os.WriteFile(path, before, 0600); err != nil {
		t.Fatal(err)
	}
	if resumed, err := OpenSubagent("parent", "child", "/other", true); err == nil {
		resumed.Close()
		t.Fatal("workspace mismatch accepted")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("rejected resume repaired the transcript before validating ownership")
	}
}

func TestResumeToleratesShortObservationLock(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	child, err := OpenSubagent("parent", "child", "/project", false)
	if err != nil {
		t.Fatal(err)
	}
	child.Begin("explore", "inspect", nil)
	if err := child.Checkpoint(ctxmgr.ManagerSnapshot{}, "running", ""); err != nil {
		t.Fatal(err)
	}
	child.Close()
	root, _ := subagentRoot("parent")
	probe, err := os.OpenFile(filepath.Join(root, "child", ".active.lock"), os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := trySubagentLock(probe); err != nil || !ok {
		probe.Close()
		t.Fatal("could not simulate observation lock", err)
	}
	done := make(chan struct{})
	go func() { defer close(done); time.Sleep(15 * time.Millisecond); probe.Close() }()
	resumed, err := OpenSubagent("parent", "child", "/project", true)
	<-done
	if err != nil {
		t.Fatalf("short observation lock rejected legal resume: %v", err)
	}
	resumed.Close()
}

func TestSubagentExclusiveOwnershipAndInterruptedRecovery(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	child, err := OpenSubagent("parent", "child", "/project", false)
	if err != nil {
		t.Fatal(err)
	}
	defer child.Close()
	if _, err := OpenSubagent("parent", "child", "/project", true); err == nil {
		t.Fatal("second writer acquired task")
	}
	if err := child.Begin("explore", "inspect", nil); err != nil {
		t.Fatal(err)
	}
	snap := ctxmgr.ManagerSnapshot{Messages: []types.Message{{Role: "user", Content: "inspect"}}, Transcript: []types.Message{{Role: "user", Content: "inspect"}}}
	if err := child.Checkpoint(snap, "running", ""); err != nil {
		t.Fatal(err)
	}
	if err := child.Close(); err != nil {
		t.Fatal(err)
	}
	resumed, err := OpenSubagent("parent", "child", "/project", true)
	if err != nil {
		t.Fatal(err)
	}
	defer resumed.Close()
	if resumed.Snapshot().Subagent.Status != "interrupted" {
		t.Fatal("stale running status not recovered as interrupted")
	}
	if err := resumed.Begin("coder", "change authority", nil); err == nil {
		t.Fatal("profile changed on resume")
	}
}

func TestSubagentRejectsTraversalAndWorkspaceMismatch(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, ids := range [][2]string{{"../escape", "child"}, {"parent", "../escape"}} {
		if child, err := OpenSubagent(ids[0], ids[1], "/project", false); err == nil {
			child.Close()
			t.Fatal("accepted path traversal")
		}
	}
	child, err := OpenSubagent("parent", "child", "/project", false)
	if err != nil {
		t.Fatal(err)
	}
	defer child.Close()
	if err := child.Begin("explore", "inspect", nil); err != nil {
		t.Fatal(err)
	}
	if err := child.Checkpoint(ctxmgr.ManagerSnapshot{}, "completed", "done"); err != nil {
		t.Fatal(err)
	}
	child.Close()
	if other, err := OpenSubagent("parent", "child", "/other", true); err == nil {
		other.Close()
		t.Fatal("accepted another workspace")
	}
}

func TestSubagentTitleIsStableAcrossResume(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	child, err := OpenSubagent("parent", "child", "/project", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := child.Begin("explore", "a long instruction used as the prompt", nil, "修复认证"); err != nil {
		t.Fatal(err)
	}
	if err := child.Checkpoint(ctxmgr.ManagerSnapshot{}, "completed", "done"); err != nil {
		t.Fatal(err)
	}
	child.Close()
	resumed, err := OpenSubagent("parent", "child", "/project", true)
	if err != nil {
		t.Fatal(err)
	}
	defer resumed.Close()
	if err := resumed.Begin("explore", "continue", nil, "不应覆盖原标题"); err != nil {
		t.Fatal(err)
	}
	if got := resumed.Snapshot().Subagent.Title; got != "修复认证" {
		t.Fatalf("title changed on resume: %q", got)
	}
	info, err := ReadSubagentMetadata("parent", "child")
	if err != nil || info.Title != "修复认证" {
		t.Fatalf("title missing from observation: %+v, %v", info, err)
	}
}

func TestSubagentAttemptsPreserveResultsUsageAndRanges(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	child, err := OpenSubagent("parent", "child", "/project", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := child.Begin("explore", "first", nil); err != nil {
		t.Fatal(err)
	}
	first := ctxmgr.ManagerSnapshot{Messages: []types.Message{{Role: "user", Content: "first"}}, Transcript: []types.Message{{Role: "user", Content: "first"}}, TranscriptLazy: true}
	if err := child.Checkpoint(first, "completed", "first result", SubagentAttemptUsage{TotalTokens: 11}); err != nil {
		t.Fatal(err)
	}
	if err := child.Close(); err != nil {
		t.Fatal(err)
	}

	resumed, err := OpenSubagent("parent", "child", "/project", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := resumed.Begin("explore", "second", nil); err != nil {
		t.Fatal(err)
	}
	second := ctxmgr.ManagerSnapshot{Messages: []types.Message{{Role: "user", Content: "first"}, {Role: "user", Content: "second"}}, Transcript: []types.Message{{Role: "user", Content: "second"}}, TranscriptBase: 1, TranscriptLazy: true}
	if err := resumed.Checkpoint(second, "completed", "second result", SubagentAttemptUsage{TotalTokens: 22}); err != nil {
		t.Fatal(err)
	}
	if err := resumed.Close(); err != nil {
		t.Fatal(err)
	}

	page, err := SubagentAttempts("parent", "child", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Attempts) != 2 {
		t.Fatalf("attempts = %d, want 2", len(page.Attempts))
	}
	firstAttempt, secondAttempt := page.Attempts[0], page.Attempts[1]
	if firstAttempt.ID == "" || secondAttempt.ID == "" || firstAttempt.ID == secondAttempt.ID || firstAttempt.Seq != 1 || secondAttempt.Seq != 2 {
		t.Fatalf("invalid attempt identities: %+v %+v", firstAttempt, secondAttempt)
	}
	if firstAttempt.Result != "first result" || secondAttempt.Result != "second result" || firstAttempt.TotalTokens != 11 || secondAttempt.TotalTokens != 22 {
		t.Fatalf("attempt result or usage overwritten: %+v %+v", firstAttempt, secondAttempt)
	}
	if firstAttempt.TranscriptStart != 1 || firstAttempt.TranscriptEnd != 1 || secondAttempt.TranscriptStart != 2 || secondAttempt.TranscriptEnd != 2 {
		t.Fatalf("invalid transcript ranges: %+v %+v", firstAttempt, secondAttempt)
	}
}

func TestFirstCheckpointCrashRecoversTailAsInterruptedAttempt(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	child, err := OpenSubagent("parent", "child", "/project", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := child.Begin("explore", "first", nil); err != nil {
		t.Fatal(err)
	}
	first := ctxmgr.ManagerSnapshot{Messages: []types.Message{{Role: "user", Content: "first"}}, Transcript: []types.Message{{Role: "user", Content: "first"}}, TranscriptLazy: true}
	if err := child.Checkpoint(first, "completed", "done"); err != nil {
		t.Fatal(err)
	}
	if err := child.Close(); err != nil {
		t.Fatal(err)
	}

	resumed, err := OpenSubagent("parent", "child", "/project", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := resumed.Begin("explore", "crashed next attempt", nil); err != nil {
		t.Fatal(err)
	}
	root, _ := subagentRoot("parent")
	tail := []types.Message{{Role: "user", Content: "crashed next attempt"}}
	if _, err := appendTranscriptFrom(filepath.Join(root, "child"), 1, 1, tail); err != nil {
		t.Fatal(err)
	}
	// Closing without a new session.json commit leaves the previous completed
	// metadata next to the valid first-checkpoint tail.
	if err := resumed.Close(); err != nil {
		t.Fatal(err)
	}

	recovered, err := OpenSubagent("parent", "child", "/project", true)
	if err != nil {
		t.Fatal(err)
	}
	meta := recovered.Snapshot().Subagent
	if len(meta.Attempts) != 2 || meta.CurrentAttempt().Seq != 2 || meta.CurrentAttempt().Status != "interrupted" || meta.CurrentAttempt().TranscriptStart != 2 || meta.CurrentAttempt().TranscriptEnd != 2 {
		t.Fatalf("orphan tail was not recovered as an attempt: %+v", meta.Attempts)
	}
	if err := recovered.Begin("explore", "third", nil); err != nil {
		t.Fatal(err)
	}
	if got := recovered.Snapshot().Subagent.CurrentAttempt(); got.Seq != 3 || got.TranscriptStart != 3 {
		t.Fatalf("next attempt overlaps recovered tail: %+v", got)
	}
	defer recovered.Close()
}

func TestLegacySubagentDoesNotBackfillAttempts(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	child, err := OpenSubagent("parent", "child", "/project", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := child.Begin("explore", "legacy", nil); err != nil {
		t.Fatal(err)
	}
	if err := child.Checkpoint(ctxmgr.ManagerSnapshot{}, "completed", "legacy result"); err != nil {
		t.Fatal(err)
	}
	if err := child.Close(); err != nil {
		t.Fatal(err)
	}
	root, _ := subagentRoot("parent")
	path := filepath.Join(root, "child", "session.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var saved map[string]any
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	metadata := saved["subagent"].(map[string]any)
	delete(metadata, "attempts")
	delete(metadata, "current_attempt_id")
	data, _ = json.Marshal(saved)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	resumed, err := OpenSubagent("parent", "child", "/project", true)
	if err != nil {
		t.Fatal(err)
	}
	if got := resumed.Snapshot().Subagent.CurrentAttempt(); got != nil {
		t.Fatalf("legacy history was guessed as an attempt: %+v", got)
	}
	if err := resumed.Begin("explore", "new execution", nil); err != nil {
		t.Fatal(err)
	}
	attempt := resumed.Snapshot().Subagent.CurrentAttempt()
	if attempt == nil || attempt.Seq != 2 || len(resumed.Snapshot().Subagent.Attempts) != 1 {
		t.Fatalf("new attempt after legacy record is invalid: %+v", attempt)
	}
	if err := resumed.Checkpoint(ctxmgr.ManagerSnapshot{}, "completed", "new result"); err != nil {
		t.Fatal(err)
	}
	defer resumed.Close()
}

func TestSubagentRecoveryKeepsValidTailInInterruptedAttempt(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	child, err := OpenSubagent("parent", "child", "/project", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := child.Begin("explore", "first", nil); err != nil {
		t.Fatal(err)
	}
	initial := ctxmgr.ManagerSnapshot{Messages: []types.Message{{Role: "user", Content: "first"}}, Transcript: []types.Message{{Role: "user", Content: "first"}}, TranscriptLazy: true}
	if err := child.Checkpoint(initial, "running", ""); err != nil {
		t.Fatal(err)
	}
	root, _ := subagentRoot("parent")
	tail := []types.Message{{Role: "assistant", ToolCalls: []types.ToolCall{{ID: "unfinished", Function: types.FunctionCall{Name: "shell", Arguments: "{}"}}}}}
	if _, err := appendTranscriptFrom(filepath.Join(root, "child"), 1, 1, tail); err != nil {
		t.Fatal(err)
	}
	if err := child.Close(); err != nil {
		t.Fatal(err)
	}
	count, err := newHistoryStore(filepath.Join(root, "child")).Count(1, true)
	if err != nil || count != 2 {
		t.Fatalf("valid tail count = %d, err = %v", count, err)
	}

	resumed, err := OpenSubagent("parent", "child", "/project", true)
	if err != nil {
		t.Fatal(err)
	}
	previous := resumed.Snapshot().Subagent.CurrentAttempt()
	if previous == nil || previous.Status != "interrupted" || previous.TranscriptEnd != 2 {
		t.Fatalf("valid tail not retained in interrupted attempt: %+v", previous)
	}
	if err := resumed.Begin("explore", "second", nil); err != nil {
		t.Fatal(err)
	}
	next := ctxmgr.ManagerSnapshot{Messages: []types.Message{{Role: "user", Content: "second"}}, Transcript: []types.Message{{Role: "user", Content: "second"}}, TranscriptBase: 2, TranscriptLazy: true}
	if err := resumed.Checkpoint(next, "running", ""); err != nil {
		t.Fatal(err)
	}
	defer resumed.Close()
	meta := resumed.Snapshot().Subagent
	if len(meta.Attempts) != 2 || meta.Attempts[0].TranscriptEnd != 2 || meta.Attempts[1].TranscriptStart != 3 || meta.Attempts[1].TranscriptEnd != 3 {
		t.Fatalf("recovery mixed attempt ranges: %+v", meta.Attempts)
	}
}

func TestSavedExecutorCeilingCannotExpand(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	child, err := OpenSubagent("parent", "executor", "/workspace", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := child.SetToolCeiling([]string{"read", "mcp__demo__lookup"}); err != nil {
		t.Fatal(err)
	}
	if err := child.Begin("executor", "inspect", nil); err != nil {
		t.Fatal(err)
	}
	if err := child.Checkpoint(ctxmgr.ManagerSnapshot{}, "completed", "done"); err != nil {
		t.Fatal(err)
	}
	snapshot := child.Snapshot()
	snapshot.Subagent.ToolCeiling[0] = "shell"
	if child.Snapshot().Subagent.ToolCeiling[0] != "read" {
		t.Fatal("snapshot mutated task capabilities")
	}
	child.Close()
	metadata, err := ReadSubagentMetadata("parent", "executor")
	if err != nil || metadata.ToolCeiling == nil {
		t.Fatalf("capability ceiling lost through metadata observation cache: %+v %v", metadata, err)
	}
	restored, err := OpenSubagent("parent", "executor", "/workspace", true)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if err := restored.SetToolCeiling([]string{"read", "shell"}); err == nil {
		t.Fatal("resumed task expanded capabilities")
	}
	if err := restored.SetToolCeiling([]string{"read"}); err != nil {
		t.Fatal(err)
	}
}

func TestEmptyExecutorCeilingRemainsCaptured(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	child, err := OpenSubagent("parent", "empty-executor", "/workspace", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := child.SetToolCeiling([]string{}); err != nil {
		t.Fatal(err)
	}
	child.Begin("executor", "report missing capabilities", nil)
	if err := child.Checkpoint(ctxmgr.ManagerSnapshot{}, "completed", "no tools"); err != nil {
		t.Fatal(err)
	}
	child.Close()
	info, err := ReadSubagentMetadata("parent", "empty-executor")
	if err != nil {
		t.Fatal(err)
	}
	if info.ToolCeiling == nil || len(info.ToolCeiling) != 0 {
		t.Fatalf("empty captured capability set became legacy nil: %+v", info)
	}
	restored, err := OpenSubagent("parent", "empty-executor", "/workspace", true)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if err := restored.SetToolCeiling([]string{"read"}); err == nil {
		t.Fatal("empty saved ceiling acquired a capability")
	}
}

func TestTaskUpdateReceiptRecoversFromUncommittedTranscriptTail(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	child, err := OpenSubagent("parent", "child", "/project", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := child.Begin("explore", "inspect", nil); err != nil {
		t.Fatal(err)
	}
	if err := child.Checkpoint(ctxmgr.ManagerSnapshot{}, "running", ""); err != nil {
		t.Fatal(err)
	}
	root, err := subagentRoot("parent")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "child", "session.json")
	oldManifest, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	manager := ctxmgr.New(ctxmgr.Config{})
	manager.AddTaskUpdate(7, "exactly one user update")
	if err := child.Checkpoint(manager.Snapshot(), "running", ""); err != nil {
		t.Fatal(err)
	}
	metadata, err := ReadSubagentMetadata("parent", "child")
	if err != nil || metadata.TaskUpdateSequence != 7 {
		t.Fatalf("canonical update floor lost: %+v %v", metadata, err)
	}
	if err := child.Close(); err != nil {
		t.Fatal(err)
	}
	// Transcript append committed, but the receipt in session.json did not.
	if err := os.WriteFile(path, oldManifest, 0600); err != nil {
		t.Fatal(err)
	}
	recovered, err := OpenSubagent("parent", "child", "/project", true)
	if err != nil {
		t.Fatal(err)
	}
	defer recovered.Close()
	snapshot := recovered.Snapshot().ContextSnapshot()
	restored := ctxmgr.New(ctxmgr.Config{})
	restored.Restore(snapshot)
	restored.AddTaskUpdate(7, "exactly one user update")
	got := restored.Snapshot()
	if got.TaskUpdateSequence != 7 {
		t.Fatalf("receipt was lost: %d", got.TaskUpdateSequence)
	}
	count := 0
	for _, message := range got.Messages {
		if message.Content == "exactly one user update" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("uncommitted tail replayed %d times", count)
	}
}

func TestToolCeilingPersistsBeforeBeginAndRejectsClosedSession(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	child, err := OpenSubagent("parent", "early-ceiling", "/workspace", false)
	if err != nil {
		t.Fatal(err)
	}
	if err = child.SetToolCeiling([]string{"read"}); err != nil {
		t.Fatal(err)
	}
	metadata, err := ReadSubagentMetadata("parent", "early-ceiling")
	if err != nil || len(metadata.ToolCeiling) != 1 || metadata.ToolCeiling[0] != "read" {
		t.Fatalf("ceiling not saved: %+v %v", metadata, err)
	}
	child.Close()
	if err = child.SetToolCeiling([]string{}); err == nil {
		t.Fatal("closed session accepted mutation")
	}
	var empty SubagentSession
	if err = empty.SetToolCeiling(nil); err == nil {
		t.Fatal("unopened session accepted mutation")
	}
}

func TestToolCeilingSaveFailureRestoresPreviousScope(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	child, err := OpenSubagent("parent", "ceiling-rollback", "/workspace", false)
	if err != nil {
		t.Fatal(err)
	}
	defer child.Close()
	if err = child.SetToolCeiling([]string{"read", "list"}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(child.root, child.snapshot.ID, "session.json")
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err = child.SetToolCeiling([]string{"read"}); err == nil {
		t.Fatal("expected persistence failure")
	}
	if got := child.snapshot.Subagent.ToolCeiling; len(got) != 2 || got[1] != "list" {
		t.Fatalf("failed save changed scope: %v", got)
	}
}

func TestSubagentDemandOwnershipPersistsBeforeFirstAttempt(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	child, err := OpenSubagent("parent", "owned", "/workspace", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := child.SetGroupID("original-plan"); err != nil {
		t.Fatal(err)
	}
	child.Close()
	meta, err := ReadSubagentMetadata("parent", "owned")
	if err != nil || meta.GroupID != "original-plan" {
		t.Fatalf("lost ownership before checkpoint: %+v %v", meta, err)
	}
	restored, err := OpenSubagent("parent", "owned", "/workspace", true)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if err := restored.SetGroupID("original-plan"); err != nil {
		t.Fatal(err)
	}
	if err := restored.SetGroupID("another-plan"); err == nil {
		t.Fatal("resumed task changed demand")
	}
	if restored.Snapshot().Subagent.GroupID != "original-plan" {
		t.Fatal("failed change mutated ownership")
	}
}
