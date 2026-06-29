package runtime

import (
	"os"
	"strings"

	"nekocode/bot/debug"
	"nekocode/bot/governance"
	"nekocode/bot/hooks"
	"nekocode/bot/tools"
)

type filteredToolCalls struct {
	allowed      []tools.ToolCallItem
	blocked      map[int]string
	preToolHints []*hooks.Hint
}

func (a *Agent) filterToolCalls(calls []tools.ToolCallItem, state *stepState) filteredToolCalls {
	out := filteredToolCalls{
		allowed: make([]tools.ToolCallItem, 0, len(calls)),
		blocked: make(map[int]string),
	}
	for i, c := range calls {
		if msg := a.preEditBlockReason(c); msg != "" {
			out.blocked[i] = msg
			out.preToolHints = append(out.preToolHints, &hooks.Hint{
				Type:     "pre_edit_guard",
				Severity: "warning",
				Content:  msg,
			})
			debug.Log("pre-edit guard: blocked %s — %s", c.Name, msg)
			continue
		}

		if err := state.quota.ConsumeCall(c.Name, c.Args); err != nil {
			out.blocked[i] = err.Error()
			debug.Log("quota: blocked %s — %v", c.Name, err)
			continue
		}

		if a.applyPreToolPolicy(c, out.blocked, i, &out.preToolHints) {
			continue
		}

		out.allowed = append(out.allowed, c)
	}
	return out
}

func (a *Agent) applyPreToolPolicy(c tools.ToolCallItem, blocked map[int]string, idx int, hints *[]*hooks.Hint) bool {
	if a.gov == nil || a.gov.HookReg == nil {
		return false
	}
	shouldBlock := false
	for _, r := range a.gov.HookReg.Evaluate(hooks.PreToolUse, c.Name, false, c.Args) {
		if r.Hint != nil {
			*hints = append(*hints, r.Hint)
		}
		if r.BlockTool != nil && (r.BlockTool.Tool == "" || r.BlockTool.Tool == c.Name) {
			blocked[idx] = r.BlockTool.Reason
			if blocked[idx] == "" {
				blocked[idx] = PolicyBlockedDefault
			}
			debug.Log("policy: blocked %s — %s", c.Name, blocked[idx])
			shouldBlock = true
		}
		if r.Stop != nil {
			blocked[idx] = PolicyBlockedStop(r.Stop.String())
			shouldBlock = true
		}
	}
	return shouldBlock
}

func emitToolStartCallbacks(calls []tools.ToolCallItem, blocked map[int]string, callback RunCallback) {
	if callback == nil {
		return
	}
	for i, c := range calls {
		action := "tool_start"
		if _, ok := blocked[i]; ok {
			action = "tool_blocked"
		}
		preview, _ := c.Args["_preview"].(string)
		callback(action, c.Name, tools.FormatArgs(c.Args), preview)
	}
}

// -- pre-edit guard ---------------------------------------------------------

func (a *Agent) preEditBlockReason(tc tools.ToolCallItem) string {
	if a.gov == nil || a.gov.Ledger == nil {
		return ""
	}
	if tc.Name != "edit" && tc.Name != "write" {
		return ""
	}
	targetPath := extractTargetPath(tc.Name, tc.Args)
	if targetPath == "" || a.gov.Ledger.WasRead(targetPath) {
		return ""
	}
	if tc.Name == "edit" && hasSufficientEditAnchor(tc.Args) {
		return ""
	}
	resolved, err := tools.ValidatePath(targetPath)
	if err != nil {
		return ""
	}
	if _, err := os.Stat(resolved); err != nil {
		return ""
	}
	return governance.ReadBeforeWriteWarning(targetPath)
}

func hasSufficientEditAnchor(args map[string]any) bool {
	oldString, _ := args["oldString"].(string)
	oldString = strings.TrimSpace(oldString)
	if oldString == "" {
		return false
	}
	if len([]rune(oldString)) >= 200 {
		return true
	}
	lines := strings.Split(oldString, "\n")
	nonEmpty := 0
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			nonEmpty++
		}
	}
	return nonEmpty >= 5
}

func extractTargetPath(toolName string, args map[string]any) string {
	switch toolName {
	case "write", "edit":
		p, _ := args["path"].(string)
		return p
	}
	return ""
}
