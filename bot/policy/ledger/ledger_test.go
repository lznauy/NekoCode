package ledger

import (
	"testing"

	"nekocode/bot/policy/semantics"
)

func TestLedgerRecordsModificationAndVerification(t *testing.T) {
	l := New()
	l.RecordTool(ToolEvent{
		Name:      "write",
		Args:      map[string]any{"path": "x.go"},
		Semantics: semantics.ClassifyToolCall("write", nil),
	})
	l.RecordTool(ToolEvent{
		Name:      "bash",
		Args:      map[string]any{"command": "go test ./..."},
		Output:    "ok",
		Semantics: semantics.ClassifyToolCall("bash", map[string]any{"command": "go test ./..."}),
	})

	snap := l.Snapshot()
	if !snap.HasModifications() {
		t.Fatal("expected modification")
	}
	if !snap.HasPassingVerification() {
		t.Fatal("expected passing verification")
	}
}

func TestWasRead(t *testing.T) {
	l := New()

	// Not read yet
	if l.WasRead("/tmp/test.go") {
		t.Error("file not read → should return false")
	}

	// Record a read (via a read tool event that has SourceProducing)
	l.RecordTool(ToolEvent{
		Name:      "read",
		Args:      map[string]any{"path": "/tmp/test.go"},
		Semantics: semantics.Semantics{SourceProducing: true},
	})

	if !l.WasRead("/tmp/test.go") {
		t.Error("file was read → should return true")
	}

	// Path cleaning should normalize
	if !l.WasRead("/tmp/foo/../test.go") {
		t.Error("cleaned path should match")
	}

	// Different file not read
	if l.WasRead("/tmp/other.go") {
		t.Error("unrelated file → should return false")
	}
}

func TestLedgerRecordsBashReadPaths(t *testing.T) {
	l := New()
	l.RecordTool(ToolEvent{
		Name:      "bash",
		Args:      map[string]any{"command": "cat bot/agent/ledger/ledger.go"},
		Semantics: semantics.ClassifyToolCall("bash", map[string]any{"command": "cat bot/agent/ledger/ledger.go"}),
	})
	l.RecordTool(ToolEvent{
		Name:      "bash",
		Args:      map[string]any{"command": "rg -n WasRead bot/agent/ledger/ledger_test.go"},
		Semantics: semantics.ClassifyToolCall("bash", map[string]any{"command": "rg -n WasRead bot/agent/ledger/ledger_test.go"}),
	})

	if !l.WasRead("bot/agent/ledger/ledger.go") {
		t.Fatal("cat path should be recorded as read")
	}
	if !l.WasRead("bot/agent/ledger/ledger_test.go") {
		t.Fatal("rg path should be recorded as read")
	}
}
