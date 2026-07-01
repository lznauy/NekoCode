package runtime

import (
	"os"
	"strings"
	"testing"

	"nekocode/bot/policy/ledger"
	"nekocode/bot/policy/semantics"
	"nekocode/bot/tools/core"
)

func TestPreEditBlockReasonRequiresLedgerReadForExistingFile(t *testing.T) {
	a := newTestAgent()
	path := t.TempDir() + "/target.go"
	if err := os.WriteFile(path, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tc := core.ToolCallItem{Name: "write", Args: map[string]any{"path": path}}
	if got := preToolBlockReasonForTest(a, tc); got == "" {
		t.Fatal("expected existing unread file to be blocked")
	}

	a.deps.gov.Ledger.RecordTool(ledger.ToolEvent{
		Name:      "read",
		Args:      map[string]any{"path": path},
		Semantics: semantics.ClassifyToolCall("read", map[string]any{"path": path}),
	})
	if got := preToolBlockReasonForTest(a, tc); got != "" {
		t.Fatalf("expected read file to pass, got %q", got)
	}
}

func TestPreEditBlockReasonAllowsEditWithSufficientAnchor(t *testing.T) {
	a := newTestAgent()
	path := t.TempDir() + "/target.go"
	if err := os.WriteFile(path, []byte("package main\n\nfunc main() {\n\tmessage := \"hello\"\n\tprintln(message)\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tc := core.ToolCallItem{Name: "edit", Args: map[string]any{
		"path": path,
		"oldString": strings.Join([]string{
			"package main",
			"",
			"func main() {",
			"\tmessage := \"hello\"",
			"\tprintln(message)",
			"}",
		}, "\n"),
		"newString": "package main\n",
	}}
	if got := preToolBlockReasonForTest(a, tc); got != "" {
		t.Fatalf("expected sufficiently anchored edit to pass without read, got %q", got)
	}
}

func TestPreEditBlockReasonBlocksEditWithShortAnchor(t *testing.T) {
	a := newTestAgent()
	path := t.TempDir() + "/target.go"
	if err := os.WriteFile(path, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tc := core.ToolCallItem{Name: "edit", Args: map[string]any{
		"path":      path,
		"oldString": "main",
		"newString": "app",
	}}
	if got := preToolBlockReasonForTest(a, tc); got == "" {
		t.Fatal("expected short unread edit to be blocked")
	}
}

func TestPreEditBlockReasonAllowsNewFile(t *testing.T) {
	a := newTestAgent()
	path := t.TempDir() + "/new.go"
	tc := core.ToolCallItem{Name: "write", Args: map[string]any{"path": path}}
	if got := preToolBlockReasonForTest(a, tc); got != "" {
		t.Fatalf("expected new file write to pass, got %q", got)
	}
}
