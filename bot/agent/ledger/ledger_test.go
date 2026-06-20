package ledger

import (
	"testing"

	"nekocode/bot/governance"
)

func TestLedgerRecordsModificationAndVerification(t *testing.T) {
	l := New()
	l.RecordTool(ToolEvent{
		Name:      "write",
		Args:      map[string]any{"path": "x.go"},
		Semantics: governance.ClassifyToolCall("write", nil),
	})
	l.RecordTool(ToolEvent{
		Name:      "bash",
		Args:      map[string]any{"command": "go test ./..."},
		Output:    "ok",
		Semantics: governance.ClassifyToolCall("bash", map[string]any{"command": "go test ./..."}),
	})

	snap := l.Snapshot()
	if !snap.HasModifications() {
		t.Fatal("expected modification")
	}
	if !snap.HasPassingVerification() {
		t.Fatal("expected passing verification")
	}
}

func TestFinalCheckRequiresVerificationForModifiedFiles(t *testing.T) {
	l := New()
	l.RecordTool(ToolEvent{
		Name:      "write",
		Args:      map[string]any{"path": "x.go"},
		Semantics: governance.ClassifyToolCall("write", nil),
	})

	issues := CheckFinalAnswer("已完成修复。", l.Snapshot())
	if len(issues) == 0 {
		t.Fatal("expected final check issue")
	}

	issues = CheckFinalAnswer("已完成修复，但未验证。", l.Snapshot())
	if len(issues) != 0 {
		t.Fatalf("unverified disclosure should pass, got %+v", issues)
	}
}

func TestFinalCheckAllowsDocumentationOnlyChangesWithoutVerification(t *testing.T) {
	l := New()
	l.RecordTool(ToolEvent{
		Name:      "write",
		Args:      map[string]any{"path": "README.md"},
		Semantics: governance.ClassifyToolCall("write", nil),
	})
	l.RecordTool(ToolEvent{
		Name:      "edit",
		Args:      map[string]any{"patch": "[docs/usage.md#A1]\nreplace 1..1:\n+updated"},
		Semantics: governance.ClassifyToolCall("edit", nil),
	})

	if issues := CheckFinalAnswer("已更新文档。", l.Snapshot()); len(issues) != 0 {
		t.Fatalf("documentation-only changes should not require verification, got %+v", issues)
	}
}

func TestFinalCheckStillRejectsUnsupportedTestClaimForDocumentationChanges(t *testing.T) {
	l := New()
	l.RecordTool(ToolEvent{
		Name:      "write",
		Args:      map[string]any{"path": "README.md"},
		Semantics: governance.ClassifyToolCall("write", nil),
	})

	issues := CheckFinalAnswer("文档已更新，测试通过。", l.Snapshot())
	if len(issues) != 1 || issues[0].Type != "unsupported_test_claim" {
		t.Fatalf("expected unsupported test claim issue, got %+v", issues)
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
		Semantics: governance.Semantics{SourceProducing: true},
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
		Semantics: governance.ClassifyToolCall("bash", map[string]any{"command": "cat bot/agent/ledger/ledger.go"}),
	})
	l.RecordTool(ToolEvent{
		Name:      "bash",
		Args:      map[string]any{"command": "rg -n WasRead bot/agent/ledger/ledger_test.go"},
		Semantics: governance.ClassifyToolCall("bash", map[string]any{"command": "rg -n WasRead bot/agent/ledger/ledger_test.go"}),
	})

	if !l.WasRead("bot/agent/ledger/ledger.go") {
		t.Fatal("cat path should be recorded as read")
	}
	if !l.WasRead("bot/agent/ledger/ledger_test.go") {
		t.Fatal("rg path should be recorded as read")
	}
}

func TestFinalCheckRejectsUnsupportedTestClaim(t *testing.T) {
	issues := CheckFinalAnswer("测试通过。", Snapshot{})
	if len(issues) == 0 {
		t.Fatal("expected unsupported test claim issue")
	}
}
