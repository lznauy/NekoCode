package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nekocode/bot/provider/types"
)

func seedIndexedHistory(t *testing.T, count int) *HistoryStore {
	t.Helper()
	dir := t.TempDir()
	f, err := os.Create(filepath.Join(dir, transcriptFileName))
	if err != nil {
		t.Fatal(err)
	}
	enc := json.NewEncoder(f)
	for i := 1; i <= count; i++ {
		if err := enc.Encode(transcriptRecord{Seq: i, Message: types.Message{Role: "user", Content: fmt.Sprint(i)}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return newHistoryStore(dir)
}
func TestHistoryIndexPagesAndRebuild(t *testing.T) {
	s := seedIndexedHistory(t, 10000)
	if count, err := s.Count(10000, true); err != nil || count != 10000 {
		t.Fatalf("count=%d err=%v", count, err)
	}
	p, err := s.Page(0, 100, HistoryPageBytes, 10000)
	if err != nil {
		t.Fatal(err)
	}
	if p.Before != 9901 || p.End != 10000 || len(p.Messages) != 100 || p.Messages[0].Content != "9901" {
		t.Fatalf("wrong newest window: %+v", p)
	}
	if _, err := s.Append(10000, []types.Message{{Role: "user", Content: "new"}}); err != nil {
		t.Fatal(err)
	}
	older, err := s.Page(p.Before, 100, HistoryPageBytes, 10001)
	if err != nil {
		t.Fatal(err)
	}
	if older.End != 9900 || older.Messages[99].Content != "9900" {
		t.Fatal("append shifted old cursor")
	}
	if err := os.WriteFile(filepath.Join(s.dir, "transcript.idx"), []byte("broken cache"), 0600); err != nil {
		t.Fatal(err)
	}
	rebuilt, err := s.Page(p.Before, 100, HistoryPageBytes, 10001)
	if err != nil || len(rebuilt.Messages) != 100 || rebuilt.Before != older.Before {
		t.Fatalf("rebuild: %+v err=%v", rebuilt, err)
	}
}
func TestHistoryByteBudgetAndToolHeader(t *testing.T) {
	s := seedIndexedHistory(t, 1)
	header := types.Message{Role: "assistant", ToolCalls: []types.ToolCall{{ID: "call", Function: types.FunctionCall{Name: "shell", Arguments: "{}"}}}}
	if _, err := s.Append(1, []types.Message{header, {Role: "tool", ToolCallID: "call", Content: strings.Repeat("x", 4096)}, {Role: "tool", ToolCallID: "call", Content: "tail"}}); err != nil {
		t.Fatal(err)
	}
	p, err := s.Page(0, 100, 100, 4)
	if err != nil {
		t.Fatal(err)
	}
	if p.Before != 4 || len(p.Messages) != 2 || p.Messages[0].Role != "assistant" || p.Messages[1].Content != "tail" {
		t.Fatalf("tool header lost: %+v", p)
	}
	prev, err := s.Page(p.Before, 1, 100, 4)
	if err != nil || prev.Before != 3 || len(prev.Messages) != 2 {
		t.Fatalf("single large record: %+v %v", prev, err)
	}
}
func TestHistoryAppendRetryAndRecovery(t *testing.T) {
	s := seedIndexedHistory(t, 1)
	pending := []types.Message{{Role: "user", Content: "accepted"}}
	for i := 0; i < 2; i++ {
		if count, err := s.Append(1, pending); err != nil || count != 2 {
			t.Fatalf("retry count=%d err=%v", count, err)
		}
	}
	if _, err := s.Append(1, []types.Message{{Role: "user", Content: "different"}}); err == nil {
		t.Fatal("accepted conflicting retry")
	}
	f, err := os.OpenFile(filepath.Join(s.dir, transcriptFileName), os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(`{"seq":3,"message":`)
	f.Close()
	if _, err := s.Count(2, false); err == nil {
		t.Fatal("ordinary reader repaired torn data")
	}
	if count, err := s.Count(2, true); err != nil || count != 2 {
		t.Fatalf("recovery count=%d err=%v", count, err)
	}
	all, err := s.ReadRange(1, 2)
	if err != nil || all[1].Content != "accepted" {
		t.Fatalf("recovered history lost: %v", err)
	}
}
