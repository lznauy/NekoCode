package block

import (
	"strings"
	"testing"

	"nekocode/tui/styles"
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
