package block

import (
	"strings"
	"testing"

	"nekocode/interaction/tui/styles"

	"github.com/charmbracelet/x/ansi"
)

func TestRenderWriteDiffSkipsHeader(t *testing.T) {
	sty := styles.DefaultStyles()
	got := renderToolContent(ContentBlock{
		Type:     BlockTool,
		ToolName: "write",
		Content:  "[write /tmp/file.go]\n+1:package main\n",
		Done:     true,
	}, 80, &sty)

	if strings.Contains(got, "[write /tmp/file.go]") {
		t.Fatalf("write diff header should not render:\n%s", got)
	}
	if !strings.Contains(got, "package main") {
		t.Fatalf("write diff content should render:\n%s", got)
	}
}

func TestDiffToolIsPersistent(t *testing.T) {
	if !IsPersistent("diff") {
		t.Fatal("diff should be persistent")
	}
}

func TestRenderToolsSeparatesMultipleToolBlocks(t *testing.T) {
	sty := styles.DefaultStyles()
	got := RenderTools([]ContentBlock{
		{Type: BlockTool, ToolName: "shell", ToolArgs: "go test", Done: true},
		{Type: BlockTool, ToolName: "shell", ToolArgs: "go vet", Done: true},
	}, 80, &sty)

	if !strings.Contains(got, "\n\n") {
		t.Fatalf("tool blocks should be separated by a blank line:\n%s", got)
	}
}

func TestRenderToolLineWrapsLongCommand(t *testing.T) {
	sty := styles.DefaultStyles()
	cmd := "git status --short interaction/tui/components/block/block_render.go interaction/tui/components/block/block_tool.go interaction/tui/components/block/block_tool_test.go interaction/tui/styles/colors.go interaction/tui/components/processing/processing_test.go"
	got := renderToolLine(ContentBlock{
		Type:     BlockTool,
		ToolName: "shell",
		ToolArgs: cmd,
		Done:     true,
	}, 96, &sty)
	clean := ansi.Strip(got)

	if strings.Contains(clean, "…") {
		t.Fatalf("long command should wrap instead of truncate:\n%s", got)
	}
	if !strings.Contains(clean, "Ran git status --short") {
		t.Fatalf("shell tool should render as Ran with command:\n%s", got)
	}
	if !strings.Contains(clean, "processing_test.go") {
		t.Fatalf("wrapped command should keep the tail:\n%s", got)
	}
	if !strings.Contains(clean, "\n  ") {
		t.Fatalf("long command should render continuation lines:\n%s", got)
	}
	if strings.Contains(clean, "\n    │  ") {
		t.Fatalf("continuation lines should use one space after the rail:\n%s", clean)
	}
	if !strings.Contains(clean, "\n    │ interaction/tui/components/block/block_tool.go") {
		t.Fatalf("continuation line should align with one space after rail:\n%s", clean)
	}
	for _, line := range strings.Split(got, "\n") {
		if w := ansi.StringWidth(line); w > 96 {
			t.Fatalf("rendered line width = %d, want <= 96:\n%s", w, got)
		}
	}
}

func TestRenderToolLinePrefixesMultilineCommand(t *testing.T) {
	sty := styles.DefaultStyles()
	cmd := "echo one\necho two\necho three"
	got := renderToolLine(ContentBlock{
		Type:     BlockTool,
		ToolName: "shell",
		ToolArgs: cmd,
		Content:  "one\ntwo\nthree",
		Done:     true,
	}, 80, &sty)
	clean := ansi.Strip(got)

	if !strings.Contains(clean, "Ran echo one") {
		t.Fatalf("first command line should stay in the header:\n%s", clean)
	}
	if !strings.Contains(clean, "\n    │ echo two") || !strings.Contains(clean, "\n    │ echo three") {
		t.Fatalf("multiline command continuation should use the command rail:\n%s", clean)
	}
	if strings.Contains(clean, "\necho two") || strings.Contains(clean, "\necho three") {
		t.Fatalf("multiline command should not render as raw unindented lines:\n%s", clean)
	}
	if !strings.Contains(clean, "\n    └  one") {
		t.Fatalf("tool output should remain one space deeper than command continuations:\n%s", clean)
	}
}

func TestRenderToolSummaryIsBrighterThanOutput(t *testing.T) {
	sty := styles.DefaultStyles()
	if got, want := renderSummary("go test ./...", &sty), sty.Base.Render("go test ./..."); got != want {
		t.Fatalf("tool summary style = %q, want base text style %q", got, want)
	}
	if got, muted := renderSummary("go test ./...", &sty), sty.Muted.Render("go test ./..."); got == muted {
		t.Fatal("tool summary should not use the muted output style")
	}
}

func TestRenderShellContentPreservesLeadingStatusSpace(t *testing.T) {
	sty := styles.DefaultStyles()
	got := renderToolContent(ContentBlock{
		Type:     BlockTool,
		ToolName: "shell",
		Content:  " M interaction/tui/components/block/block_tool.go\n",
		Done:     true,
	}, 80, &sty)
	clean := ansi.Strip(got)

	if !strings.HasPrefix(clean, " M ") {
		t.Fatalf("shell output should preserve leading status space, got %q", clean)
	}
}

func TestRenderShellContentKeepsHeadAndTail(t *testing.T) {
	sty := styles.DefaultStyles()
	got := renderToolContent(ContentBlock{
		Type:     BlockTool,
		ToolName: "shell",
		Content:  "line 0\nline 1\nline 2\nline 3\nline 4\nline 5\nline 6\nline 7\nline 8\nline 9\nline 10\nline 11\nline 12\n",
		Done:     true,
	}, 80, &sty)
	clean := ansi.Strip(got)

	for _, want := range []string{"line 0", "line 1", "line 2", "line 3", "line 4", "line 8", "line 9", "line 10", "line 11", "line 12"} {
		if !strings.Contains(clean, want) {
			t.Fatalf("missing retained line %q:\n%s", want, clean)
		}
	}
	for _, notWant := range []string{"line 5", "line 6", "line 7"} {
		if strings.Contains(clean, notWant) {
			t.Fatalf("middle line %q should be hidden:\n%s", notWant, clean)
		}
	}
	if !strings.Contains(clean, "3 lines truncated") {
		t.Fatalf("missing truncation marker:\n%s", clean)
	}
	lines := strings.Split(clean, "\n")
	for i, line := range lines {
		if strings.Contains(line, "lines truncated") && i+1 < len(lines) {
			if strings.TrimRight(lines[i+1], " ") != "line 8" {
				t.Fatalf("first tail line has unexpected indentation: %q\n%s", lines[i+1], clean)
			}
			return
		}
	}
	t.Fatal("truncation marker line not found")
}

func TestRenderToolOutputUsesCornerConnector(t *testing.T) {
	sty := styles.DefaultStyles()
	got := renderToolLine(ContentBlock{
		Type:     BlockTool,
		ToolName: "shell",
		ToolArgs: `echo "hello" && date`,
		Content:  "hello\nFri Jul  3 00:41:48 CST 2026",
		Done:     true,
	}, 80, &sty)
	clean := ansi.Strip(got)

	if !strings.Contains(clean, "\n    └  hello") {
		t.Fatalf("shell output should start with a corner connector:\n%s", clean)
	}
	if strings.Contains(clean, "\n    │  hello") || strings.Contains(clean, "\n    │  Fri") {
		t.Fatalf("shell output should not use a vertical rail:\n%s", clean)
	}

	got = renderToolLine(ContentBlock{
		Type:     BlockTool,
		ToolName: "write",
		ToolArgs: "/tmp/report.json",
		Content:  "(wrote 1234 bytes)",
		Done:     true,
	}, 80, &sty)
	clean = ansi.Strip(got)
	if !strings.Contains(clean, "\n    └  (wrote 1234 bytes)") {
		t.Fatalf("write output should use the same corner connector:\n%s", clean)
	}
	if strings.Contains(clean, "\n    │  (wrote 1234 bytes)") {
		t.Fatalf("write output should not use a vertical rail:\n%s", clean)
	}
}

func TestRenderToolLineDoesNotShowCollapseToggle(t *testing.T) {
	sty := styles.DefaultStyles()
	got := renderToolLine(ContentBlock{
		Type:     BlockTool,
		ToolName: "shell",
		ToolArgs: "echo hello",
		Content:  "hello",
		Done:     true,
	}, 80, &sty)
	clean := ansi.Strip(got)

	if strings.Contains(clean, "▾") || strings.Contains(clean, "▸") {
		t.Fatalf("tool render should not show collapse toggles:\n%s", clean)
	}
}

func TestRenderProcessListUsesActionWording(t *testing.T) {
	sty := styles.DefaultStyles()
	got := renderToolLine(ContentBlock{
		Type:       BlockTool,
		ToolName:   "process",
		ToolAction: "list",
		ToolArgs:   "managed processes",
		Content:    "(no managed processes)",
		Done:       true,
	}, 80, &sty)
	clean := ansi.Strip(got)

	if strings.Contains(clean, "Ran") {
		t.Fatalf("process list should not render as Ran:\n%s", clean)
	}
	if !strings.Contains(clean, "Listed managed processes") {
		t.Fatalf("process list should describe the action:\n%s", clean)
	}
	if !strings.Contains(clean, "No managed processes") {
		t.Fatalf("empty process list should be user-facing:\n%s", clean)
	}
}

func TestFilterFinalBlocksRetainsBashAndBatchResults(t *testing.T) {
	got := FilterFinalBlocks([]ContentBlock{{Type: BlockTool, ToolName: "bash"}, {Type: BlockTool, ToolName: "task_batch"}, {Type: BlockTool, ToolName: "read"}})
	if len(got) != 2 || got[0].ToolName != "bash" || got[1].ToolName != "task_batch" {
		t.Fatalf("final blocks: %+v", got)
	}
}
