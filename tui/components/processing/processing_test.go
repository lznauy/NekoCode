package processing

import (
	"strings"
	"testing"

	"nekocode/tui/components/block"
	"nekocode/tui/styles"
)

func TestFinishEditRevertIsNotError(t *testing.T) {
	sty := styles.DefaultStyles()
	p := NewProcessingItem(&sty)
	p.SetBlocks([]block.ContentBlock{{
		Type:     block.BlockTool,
		ToolName: "edit",
	}})

	p.finishToolBlock("", "edit", "[/tmp/file.txt#revert]\n-1:changed\n+1:original\n")

	blocks := p.Blocks()
	if len(blocks) != 1 {
		t.Fatalf("blocks = %d, want 1", len(blocks))
	}
	if !blocks[0].Done {
		t.Fatal("revert block was not marked done")
	}
	if blocks[0].IsError {
		t.Fatal("revert block was marked as error")
	}
}

func TestBlockedPersistentToolRendersErrorContent(t *testing.T) {
	sty := styles.DefaultStyles()
	p := NewProcessingItem(&sty)
	p.SetBlocks([]block.ContentBlock{{
		Type:     block.BlockTool,
		ToolName: "edit",
		Content:  "你正在修改 x.go，但 ledger 中没有该文件的读取记录。",
		Done:     true,
		IsError:  true,
	}})

	rendered := p.renderChangesSection(100)
	if !strings.Contains(rendered, "ledger") {
		t.Fatalf("blocked edit reason not rendered:\n%s", rendered)
	}
	if !strings.Contains(rendered, "error") {
		t.Fatalf("blocked edit status not rendered as error:\n%s", rendered)
	}
}
