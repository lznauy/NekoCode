package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/provider/types"
)

func seedObservedSubagent(t *testing.T, messages []types.Message) *SubagentSession {
	t.Helper()
	child, err := OpenSubagent("parent", "child", "/workspace", false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { child.Close() })
	if err := child.Begin("explore", "inspect", nil); err != nil {
		t.Fatal(err)
	}
	if err := child.Checkpoint(ctxmgr.ManagerSnapshot{Messages: messages, Transcript: messages}, "running", ""); err != nil {
		t.Fatal(err)
	}
	return child
}

func TestMissingSubagentLockInterruptsLatestAttempt(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	child := seedObservedSubagent(t, []types.Message{{Role: "user", Content: "inspect"}})
	if err := child.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(child.root, "child", ".active.lock")); err != nil {
		t.Fatal(err)
	}
	info, err := ReadSubagentMetadata("parent", "child")
	if err != nil {
		t.Fatal(err)
	}
	if info.Status != "interrupted" || info.LatestAttempt == nil || info.LatestAttempt.Status != "interrupted" {
		t.Fatalf("task and latest attempt must both be interrupted: %+v", info)
	}
}

func TestSubagentHistoryReadsDuringCheckpoint(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	messages := []types.Message{{Role: "user", Content: "inspect"}}
	child := seedObservedSubagent(t, messages)
	done := make(chan error, 1)
	go func() {
		for i := range 20 {
			messages = append(messages, types.Message{Role: "assistant", Content: fmt.Sprintf("progress-%d", i)})
			if err := child.Checkpoint(ctxmgr.ManagerSnapshot{Messages: messages, Transcript: messages}, "running", ""); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	var observationErr error
	for {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
			if observationErr != nil {
				t.Fatal(observationErr)
			}
			return
		default:
			page, err := SubagentHistory("parent", "child", 0, 5)
			if err != nil {
				observationErr = err
				continue
			}
			if page.End != page.Total || len(page.Messages) > 5 {
				observationErr = fmt.Errorf("invalid live page: %+v", page)
			}
		}
	}
}

func TestSubagentObserverIgnoresUncommittedTornTail(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	child := seedObservedSubagent(t, []types.Message{{Role: "user", Content: "inspect"}, {Role: "assistant", Content: "progress"}})
	path := filepath.Join(child.root, "child", transcriptFileName)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"seq":3,"message":`); err != nil {
		t.Fatal(err)
	}
	f.Close()
	before, _ := os.ReadFile(path)
	page, err := SubagentHistory("parent", "child", 0, 10)
	if err != nil || page.Total != 2 || len(page.Messages) != 2 {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("observer modified active transcript")
	}
	infos, err := ListSubagents("parent")
	if err != nil || len(infos) != 1 || infos[0].Status != "running" {
		t.Fatalf("infos=%+v err=%v", infos, err)
	}
	child.Close()
	infos, err = ListSubagents("parent")
	if err != nil || infos[0].Status != "interrupted" {
		t.Fatalf("stale running task not detected: %+v %v", infos, err)
	}
}

func TestSubagentHistoryPaginationPreservesToolOwnerAndCursor(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	messages := []types.Message{
		{Role: "user", Content: "inspect"},
		{Role: "assistant", Content: "reading", ToolCalls: []types.ToolCall{{ID: "read", Function: types.FunctionCall{Name: "read", Arguments: `{"path":"a.go"}`}}}},
		{Role: "tool", ToolCallID: "read", Content: "source"},
		{Role: "assistant", Content: "done"},
	}
	seedObservedSubagent(t, messages)
	last, err := SubagentHistory("parent", "child", 0, 1)
	if err != nil || last.Before != 4 || len(last.Messages) != 1 {
		t.Fatalf("last=%+v err=%v", last, err)
	}
	previous, err := SubagentHistory("parent", "child", last.Before, 1)
	if err != nil || previous.Before != 3 || len(previous.Messages) != 2 || previous.Messages[0].ToolCalls[0].ID != "read" || previous.Messages[1].ToolCallID != "read" {
		t.Fatalf("previous=%+v err=%v", previous, err)
	}
	first, err := SubagentHistory("parent", "child", previous.Before, 2)
	if err != nil || first.Before != 1 || first.HasMore || len(first.Messages) != 2 {
		t.Fatalf("first=%+v err=%v", first, err)
	}
}

func TestSubagentProjectionCacheCanBeDiscardedOrStale(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	child := seedObservedSubagent(t, []types.Message{{Role: "user", Content: "inspect"}})
	projection := filepath.Join(child.root, "child", "observation.json")
	if err := os.WriteFile(projection, []byte(`{"source_size":0}`), 0600); err != nil {
		t.Fatal(err)
	}
	infos, err := ListSubagents("parent")
	if err != nil || len(infos) != 1 || infos[0].Profile != "explore" {
		t.Fatalf("stale cache: %+v %v", infos, err)
	}
	if err := os.Remove(projection); err != nil {
		t.Fatal(err)
	}
	infos, err = ListSubagents("parent")
	if err != nil || len(infos) != 1 || infos[0].TranscriptSeq != 1 {
		t.Fatalf("missing cache: %+v %v", infos, err)
	}
	bad := filepath.Join(child.root, "broken")
	os.MkdirAll(bad, 0700)
	os.WriteFile(filepath.Join(bad, "session.json"), []byte(`invalid`), 0600)
	infos, err = ListSubagents("parent")
	if err != nil || len(infos) != 2 {
		t.Fatalf("one bad task hid all tasks: %+v %v", infos, err)
	}
}

func TestSubagentHistoryByteBudgetAndIndexFallback(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var messages []types.Message
	for range 30 {
		messages = append(messages, types.Message{Role: "assistant", Content: strings.Repeat("x", 64<<10)})
	}
	child := seedObservedSubagent(t, messages)
	// The index is optional; an unreadable index must not change the result.
	os.WriteFile(filepath.Join(child.root, "child", "transcript.idx"), []byte("broken"), 0600)
	page, err := SubagentHistory("parent", "child", 0, 100)
	if err != nil || !page.HasMore || page.Before <= 1 || page.End != 30 {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	bytes := 0
	for _, message := range page.Messages {
		data, _ := json.Marshal(message)
		bytes += len(data)
	}
	if bytes > HistoryPageBytes {
		t.Fatalf("page exceeded budget: %d", bytes)
	}
}

func TestSubagentMetadataUsesSnapshotNanosecondTime(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	child := seedObservedSubagent(t, []types.Message{{Role: "user", Content: "inspect"}})
	stat, err := os.Stat(filepath.Join(child.root, "child", "session.json"))
	if err != nil {
		t.Fatal(err)
	}
	// Both the projection cache and fallback snapshot reader carry precise time.
	for _, cached := range []bool{true, false} {
		if !cached {
			if err := os.Remove(filepath.Join(child.root, "child", "observation.json")); err != nil {
				t.Fatal(err)
			}
		}
		info, err := ReadSubagentMetadata("parent", "child")
		if err != nil {
			t.Fatal(err)
		}
		if info.UpdatedAtNano != stat.ModTime().UnixNano() {
			t.Fatalf("cached=%v timestamp=%d, want %d", cached, info.UpdatedAtNano, stat.ModTime().UnixNano())
		}
	}
}
