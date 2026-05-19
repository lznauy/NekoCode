// helpers.go — 工具参数/结果格式化辅助函数。
package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	"nekocode/tui/styles"

	"nekocode/common")

func formatBriefArgs(toolName, toolArgs string) string {
	parse := func(s string) map[string]string {
		m := make(map[string]string)
		for _, pair := range common.SplitPairs(s) {
			kv := strings.SplitN(pair, "=", 2)
			if len(kv) == 2 {
				m[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
			}
		}
		return m
	}
	args := parse(toolArgs)

	switch toolName {
	case "read", "write", "list", "tree":
		return args["path"]
	case "edit":
		return args["path"]
	case "bash":
		cmd := args["command"]
		// Only show the first line — heredocs and multi-line scripts
		// pollute the tool block display.
		if idx := strings.IndexAny(cmd, "\n\r"); idx >= 0 {
			cmd = cmd[:idx] + "…"
		}
		if len(cmd) > 60 {
			cmd = common.TruncateByRune(cmd, 57) + "…"
		}
		return cmd
	case "glob":
		return args["pattern"]
	case "grep":
		p := args["path"]
		if p != "" {
			return args["pattern"] + " " + p
		}
		return args["pattern"]
	case "web_search", "web_fetch":
		q := args["query"]
		if q == "" {
			q = args["url"]
		}
		return common.TruncateByRune(q, 60)
	case "todo_write":
		return formatTodos(args["todos"])
	case "task":
		t := args["subagent_type"]
		if t == "" {
			t = "executor"
		}
		if d := args["description"]; d != "" {
			return t + " \u00b7 " + d
		}
		p := strings.SplitN(args["prompt"], "\n", 2)[0]
		p = strings.Trim(p, " \"")
		return t + " \u00b7 " + common.TruncateByRune(p, 30)
	default:
		for _, v := range args {
			return common.TruncateByRune(v, 50)
		}
		return ""
	}
}

func tokensSummary(b BotInterface) string {
	p, c := b.TurnTokenUsage()
	return "↑" + styles.FmtTokens(p) + " ↓" + styles.FmtTokens(c)
}

func formatTodos(raw string) string {
	if raw == "" {
		return ""
	}
	var items []struct {
		Content string `json:"content"`
		Status  string `json:"status"`
	}
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return ""
	}
	if len(items) == 0 {
		return ""
	}
	return fmt.Sprintf("%d items", len(items))
}
