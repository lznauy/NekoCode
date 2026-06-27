// tui_snapshot renders all key TUI components with realistic data into plain-text files.
// Run: go run ./utils/tui_snapshot
package main

import (
	"log"
	"os"
	"path/filepath"

	"nekocode/common"
	components "nekocode/tui/components"
	"nekocode/tui/components/block"
	"nekocode/tui/components/message"
	"nekocode/tui/components/processing"
	"nekocode/tui/styles"
)

var sty = styles.DefaultStyles()

const width = 80

func main() {
	outDir := "/tmp/tui_snapshots"
	os.RemoveAll(outDir)
	os.MkdirAll(outDir, 0755)

	write(outDir, "confirm_edit.txt", renderConfirmEdit())
	write(outDir, "confirm_bash.txt", renderConfirmBash())
	write(outDir, "confirm_bash_long_python.txt", renderConfirmBashLongPython())
	write(outDir, "confirm_bash_massive.txt", renderConfirmBashMassive())
	write(outDir, "confirm_write.txt", renderConfirmWrite())
	write(outDir, "confirm_plugin.txt", renderConfirmPlugin())

	write(outDir, "block_edit_diff.txt", renderEditBlock())
	write(outDir, "block_edit_multifile.txt", renderEditMultiBlock())
	write(outDir, "block_edit_collapsed.txt", renderEditCollapsed())
	write(outDir, "block_bash_short.txt", renderBashShort())
	write(outDir, "block_bash_long.txt", renderBashLong())
	write(outDir, "block_write.txt", renderWriteBlock())
	write(outDir, "block_read_collapsed.txt", renderReadCollapsed())

	write(outDir, "processing_active.txt", renderProcessingActive())
	write(outDir, "processing_idle.txt", renderProcessingIdle())
	write(outDir, "processing_subagent.txt", renderProcessingSubAgent())

	write(outDir, "assistant_edit_diff.txt", renderAssistantWithEdit())

	log.Println("Snapshots written to", outDir)
}

func write(dir, name, content string) {
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		log.Printf("write %s: %v", name, err)
	}
}

// ── ConfirmBar ───────────────────────────────────────────────────────────────

func renderConfirmEdit() string {
	cb := components.NewConfirmBar(&sty)
	cb.SetRequest(&common.ConfirmRequest{
		ToolName: "edit",
		Args: map[string]any{
			"path": "bot/tools/builtin/tool_edit.go",
		},
		Level:    common.LevelWrite,
		Response: make(chan bool, 1),
	})
	return cb.View(width, 40)
}

func renderConfirmBash() string {
	cb := components.NewConfirmBar(&sty)
	cb.SetRequest(&common.ConfirmRequest{
		ToolName: "bash",
		Args: map[string]any{
			"command": "curl -sS https://api.example.com/data | jq '.items[] | select(.active == true)' | head -20",
		},
		Level:    common.LevelWrite,
		Response: make(chan bool, 1),
	})
	return cb.View(width, 40)
}

func renderConfirmBashLongPython() string {
	cb := components.NewConfirmBar(&sty)
	cb.SetRequest(&common.ConfirmRequest{
		ToolName: "bash",
		Args: map[string]any{
			"command": `python -c "import sys, json; data = json.loads(open('/tmp/data.json').read()); result = [item for item in data['items'] if item.get('active') and item.get('score', 0) > 50]; print(json.dumps(result, indent=2))"`,
		},
		Level:    common.LevelWrite,
		Response: make(chan bool, 1),
	})
	return cb.View(width, 80)
}

func renderConfirmBashMassive() string {
	cb := components.NewConfirmBar(&sty)
	cb.SetRequest(&common.ConfirmRequest{
		ToolName: "bash",
		Args: map[string]any{
			"command": `python3 << 'PYEOF'
import sys, json, os
from datetime import datetime
from pathlib import Path

def process_data(input_path, output_path):
    with open(input_path) as f:
        data = json.load(f)
    results = []
    for item in data.get("records", []):
        if item.get("status") == "active" and item.get("score", 0) > 50:
            normalized = {
                "id": item["id"],
                "name": item["name"].strip().title(),
                "score": item["score"],
                "processed_at": datetime.now().isoformat(),
            }
            results.append(normalized)
    results.sort(key=lambda x: x["score"], reverse=True)
    with open(output_path, "w") as f:
        json.dump(results, f, indent=2)
    print(f"Processed {len(results)} active records to {output_path}")

if __name__ == "__main__":
    process_data("/tmp/input.json", "/tmp/output.json")
PYEOF`,
		},
		Level:    common.LevelWrite,
		Response: make(chan bool, 1),
	})
	return cb.View(width, 80)
}

func renderConfirmWrite() string {
	cb := components.NewConfirmBar(&sty)
	cb.SetRequest(&common.ConfirmRequest{
		ToolName: "write",
		Args: map[string]any{
			"path": "/tmp/nekocode/generated_report.md",
		},
		Level:    common.LevelWrite,
		Response: make(chan bool, 1),
	})
	return cb.View(width, 40)
}

func renderConfirmPlugin() string {
	cb := components.NewConfirmBar(&sty)
	cb.SetRequest(&common.ConfirmRequest{
		ToolName: "/plugin install",
		Args: map[string]any{
			"source":  "github.com/example/some-skill",
			"summary": "Install github.com/example/some-skill (v1.2.3) — adds markdown linting support",
		},
		Level:    common.LevelWrite,
		Response: make(chan bool, 1),
	})
	return cb.View(width, 40)
}

// ── Tool Blocks ─────────────────────────────────────────────────────────────

func renderEditBlock() string {
	// Uses actual format from formatHunkDiff:
	//   -NNN:content → deletion
	//   +NNN:content → addition
	//    NNN:content → context
	diff := "[bot/tools/builtin/tool_edit.go#C5E8]\n 318:    fmt.Println(\"Alice active:\", alice.Active)\n-318:    fmt.Println(\"Alice active:\", alice.Active)\n+319:    fmt.Println(\"Alice active status:\", alice.Active)"
	b := block.ContentBlock{
		Type:      block.BlockTool,
		ToolName:  "edit",
		ToolArgs:  "tool_edit.go",
		Content:   diff,
		Collapsed: false,
		Done:      true,
	}
	return block.RenderTools([]block.ContentBlock{b}, width-10, &sty)
}

func renderEditMultiBlock() string {
	diff := "[bot/tools/builtin/tool_edit.go#B7F2]\n 120:    desc := \"Edit text in files using simple replacements...\"\n-120:    desc := \"Edit text in files using simple replacements...\"\n+121:    desc := \"Edit files using oldString/newString content anchors. Returns a diff preview + new TAG for chaining.\"\n 125:    maxTokens := 5000\n-125:    maxTokens := 5000\n+126:    maxTokens := 8000"
	b := block.ContentBlock{
		Type:      block.BlockTool,
		ToolName:  "edit",
		ToolArgs:  "tool_edit.go",
		Content:   diff,
		Collapsed: false,
		Done:      true,
	}
	return block.RenderTools([]block.ContentBlock{b}, width-10, &sty)
}

func renderEditCollapsed() string {
	b := block.ContentBlock{
		Type:      block.BlockTool,
		ToolName:  "edit",
		ToolArgs:  "main.go",
		Content:   "[main.go#D4E1]\n-42:    x := 1\n+43:    x := 2",
		Collapsed: true,
		Done:      true,
	}
	return block.RenderTools([]block.ContentBlock{b}, width-10, &sty)
}

func renderBashShort() string {
	b := block.ContentBlock{
		Type:      block.BlockTool,
		ToolName:  "bash",
		ToolArgs:  "go test ./...",
		Content:   "ok      nekocode/bot/tools      0.123s\nok      nekocode/tui             0.089s",
		Collapsed: false,
		Done:      true,
	}
	return block.RenderTools([]block.ContentBlock{b}, width-10, &sty)
}

func renderBashLong() string {
	b := block.ContentBlock{
		Type:      block.BlockTool,
		ToolName:  "bash",
		ToolArgs:  "ls -la /tmp",
		Content:   "total 128\ndrwxrwxrwt  20 root   root    4096 Jun 13 10:00 .\ndrwxr-xr-x  22 root   root    4096 Jun 10 08:30 ..\n-rw-r--r--   1 user   user    1234 Jun 13 09:45 log.txt\n-rw-r--r--   1 user   user    5678 Jun 13 09:46 data.csv\n-rw-r--r--   1 user   user    9012 Jun 13 09:47 output.json",
		Collapsed: false,
		Done:      true,
	}
	return block.RenderTools([]block.ContentBlock{b}, width-10, &sty)
}

func renderWriteBlock() string {
	b := block.ContentBlock{
		Type:      block.BlockTool,
		ToolName:  "write",
		ToolArgs:  "/tmp/nekocode/testing/report.json",
		Content:   "(wrote 1234 bytes)",
		Collapsed: false,
		Done:      true,
	}
	return block.RenderTools([]block.ContentBlock{b}, width-10, &sty)
}

func renderReadCollapsed() string {
	b := block.ContentBlock{
		Type:      block.BlockTool,
		ToolName:  "read",
		ToolArgs:  "main.go 1-50",
		Content:   "[main.go#A1B2]\n1:package main\n2:\n3:import (\n4:\t\"fmt\"\n5:\t\"os\"\n6:)\n7:\n8:func main() {\n9:\tfmt.Println(\"hello\")\n10:}",
		Collapsed: true,
		Done:      true,
	}
	return block.RenderTools([]block.ContentBlock{b}, width-10, &sty)
}

// ── ProcessingItem ──────────────────────────────────────────────────────────

func renderProcessingActive() string {
	p := processing.NewProcessingItem(&sty)
	p.SetSpinnerView("⠋")
	p.SetStatusText("Running edit tool_edit.go (3.2s)")
	p.SetTokens(12400, 890)
	p.SetSkill("refactor")

	// Task list
	p.SetTodos("Tasks 3/5\n· 分析代码结构\n▸ 修改edit工具\n✓ 添加snapshot测试\n· 更新文档\n· 运行测试")

	// Activity tools
	p.AddToolBlock(block.ContentBlock{
		Type: block.BlockTool, ToolName: "read", ToolArgs: "tool_edit.go 1-100",
		Content: "[tool_edit.go#A1B2]\n1:package builtin\n...", Collapsed: true, Done: true,
	})
	p.AddToolBlock(block.ContentBlock{
		Type: block.BlockTool, ToolName: "grep", ToolArgs: "Description tool_edit.go",
		Content: "120:    desc := \"Edit text in files...\"", Collapsed: true, Done: true,
	})
	p.AddToolBlock(block.ContentBlock{
		Type: block.BlockTool, ToolName: "read", ToolArgs: "tool_edit_test.go 1-80",
		Content: "[tool_edit_test.go#C3D4]\n1:package builtin\n...", Collapsed: true, Done: true,
	})

	// Changes (edit) — formatHunkDiff format
	p.AddToolBlock(block.ContentBlock{
		Type: block.BlockTool, ToolName: "edit", ToolArgs: "tool_edit.go",
		Content:   "[tool_edit.go#E5F6]\n 318:    fmt.Println(\"Alice active:\", alice.Active)\n-318:    fmt.Println(\"Alice active:\", alice.Active)\n+319:    fmt.Println(\"Alice active status:\", alice.Active)",
		Collapsed: false, Done: true,
	})

	p.AppendThinkingText("Let me update the Description to be more precise and merge overlapping rules...\nThe current description is 121 lines, target ~100.")
	p.AppendStreamText("Updated Description() to 103 lines. Removed QUICK-START duplicate, merged anti-patterns into RULES.")

	return p.Render(width)
}

func renderProcessingIdle() string {
	p := processing.NewProcessingItem(&sty)
	p.SetSpinnerView("⠿")
	p.SetStatusText("Thinking (0.8s)")
	p.SetTokens(5600, 0)

	p.AppendThinkingText("Analyzing the codebase structure...\nFound 3 files that need modification.\nThe edit tool Description is too verbose.")

	return p.Render(width)
}

// ── Sub-Agent Processing ────────────────────────────────────────────────────

func renderProcessingSubAgent() string {
	p := processing.NewProcessingItem(&sty)
	p.SetSpinnerView("⠋")
	p.SetStatusText("Running task(researcher) + task(executor) (12.5s)")
	p.SetTokens(32400, 2890)
	p.SetSkill("refactor")

	// Register two active sub-agents in header
	p.AddSubAgent("a1b2", "researcher", 0) // red: #e57373
	p.AddSubAgent("c3d4", "executor", 3)   // orange: #ffb74d

	// Task list
	p.SetTodos("Tasks 2/3\n· 分析代码结构\n▸ 修改edit工具\n✓ 重构run_exec.go")

	// Main agent tools (ฅ teal)
	p.AddToolBlock(block.ContentBlock{
		Type: block.BlockTool, ToolName: "read", ToolArgs: "run_exec.go 1-100",
		Content: "[run_exec.go#A1B2]\n1:package agent\n...", Collapsed: true, Done: true,
	})
	p.AddToolBlock(block.ContentBlock{
		Type: block.BlockTool, ToolName: "task", ToolArgs: "researcher, edit logic audit",
		Content: "Sub-agent researcher completed: found 3 issues in run_exec.go", Collapsed: true, Done: true,
	})

	// Sub-agent researcher tools (೬ red #e57373)
	p.AddToolBlock(block.ContentBlock{
		Type: block.BlockTool, ToolName: "read", ToolArgs: "run_exec.go 50-120",
		Content: "[run_exec.go#C3D4]\n50:func (a *Agent)...", Collapsed: true, Done: true,
		SubID: "a1b2", SubColor: 0,
	})
	p.AddToolBlock(block.ContentBlock{
		Type: block.BlockTool, ToolName: "grep", ToolArgs: "executeAndFeedback .",
		Content: "53:func (a *Agent) executeAndFeedback(calls...", Collapsed: true, Done: true,
		SubID: "a1b2", SubColor: 0,
	})

	// Sub-agent executor tools (೬ orange #ffb74d)
	p.AddToolBlock(block.ContentBlock{
		Type: block.BlockTool, ToolName: "read", ToolArgs: "subslot.go 1-60",
		Content: "[subslot.go#E5F6]\n1:package agent\n...", Collapsed: true, Done: true,
		SubID: "c3d4", SubColor: 3,
	})
	p.AddToolBlock(block.ContentBlock{
		Type: block.BlockTool, ToolName: "edit", ToolArgs: "run_exec.go",
		Content:   "[run_exec.go#G7H8]\n 82:    // Execute allowed tools\n-82:    // Execute allowed tools\n+82:    // Sub-agent lifecycle: inject callbacks",
		Collapsed: false, Done: true,
		SubID: "c3d4", SubColor: 3,
	})

	// Main agent continues
	p.AddToolBlock(block.ContentBlock{
		Type: block.BlockTool, ToolName: "bash", ToolArgs: "go test ./bot/agent/...",
		Content: "ok      nekocode/bot/agent      0.456s", Collapsed: false, Done: true,
	})

	p.AppendThinkingText("Sub-agent researcher found 3 issues, executor applied the fix. Reviewing the diff...")
	p.AppendStreamText("All sub-agents completed. run_exec.go now has proper sub-agent lifecycle management.")

	return p.Render(width)
}

// ── AssistantMessageItem ────────────────────────────────────────────────────

func renderAssistantWithEdit() string {
	m := message.NewAssistantMessageItem(&sty, "I've updated the Description() method to be more concise. The key changes:\n\n- Removed QUICK-START section (duplicated HEADER+EXAMPLE)\n- Merged anti-pattern #1 into RULES\n- Compressed from 121 to 103 lines\n\nThe tool still describes all operations clearly.")
	m.SetBlocks([]block.ContentBlock{
		{
			Type: block.BlockTool, ToolName: "edit", ToolArgs: "tool_edit.go",
			Content:   "[bot/tools/builtin/tool_edit.go#C5E8]\n 120:    desc := \"Edit text in files using simple replacements...\"\n-120:    desc := \"Edit text in files using simple replacements...\"\n+121:    desc := \"Edit files using oldString/newString content anchors...\"\n 125:    maxTokens := 5000\n-125:    maxTokens := 5000\n+126:    maxTokens := 8000",
			Collapsed: false, Done: true,
		},
	})
	m.SetFooter("Duration: 4.2s  ↑1.8k ↓312")
	return m.Render(width)
}
