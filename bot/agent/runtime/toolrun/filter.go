package toolrun

import (
	"os"

	"nekocode/bot/agent/runtime/messages"
	"nekocode/bot/agent/runtime/toolpolicy"
	"nekocode/bot/debug"
	"nekocode/bot/hooks"
	"nekocode/bot/policy/budget"
	"nekocode/bot/tools"
)

type FilteredCalls struct {
	Allowed      []tools.ToolCallItem
	Blocked      map[int]string
	PreToolHints []*hooks.Hint
}

func (r *Runner) FilterToolCalls(calls []tools.ToolCallItem, quota *budget.ToolQuota) FilteredCalls {
	out := FilteredCalls{
		Allowed: make([]tools.ToolCallItem, 0, len(calls)),
		Blocked: make(map[int]string),
	}
	for i, c := range calls {
		if err := quota.ConsumeCall(c.Name, c.Args); err != nil {
			out.Blocked[i] = err.Error()
			debug.Log("quota: blocked %s — %v", c.Name, err)
			continue
		}

		if r.applyPreToolPolicy(c, out.Blocked, i, &out.PreToolHints) {
			continue
		}

		out.Allowed = append(out.Allowed, c)
	}
	return out
}

func (r *Runner) applyPreToolPolicy(c tools.ToolCallItem, blocked map[int]string, idx int, hints *[]*hooks.Hint) bool {
	gov := r.host.Governance()
	if gov == nil || gov.HookReg == nil {
		return false
	}
	r.preparePreToolHookState(c)
	shouldBlock := false
	for _, result := range gov.HookReg.Evaluate(hooks.PreToolUse, c.Name, false, c.Args) {
		if result.Hint != nil {
			*hints = append(*hints, result.Hint)
		}
		if result.BlockTool != nil && (result.BlockTool.Tool == "" || result.BlockTool.Tool == c.Name) {
			blocked[idx] = result.BlockTool.Reason
			if blocked[idx] == "" {
				blocked[idx] = messages.PolicyBlockedDefault
			}
			debug.Log("policy: blocked %s — %s", c.Name, blocked[idx])
			shouldBlock = true
		}
		if result.Stop != nil {
			blocked[idx] = messages.PolicyBlockedStop(result.Stop.String())
			shouldBlock = true
		}
	}
	return shouldBlock
}

func (r *Runner) preparePreToolHookState(tc tools.ToolCallItem) {
	gov := r.host.Governance()
	if gov == nil || gov.Ledger == nil {
		return
	}
	targetPath := toolpolicy.ExtractTargetPath(tc.Name, tc.Args)
	gov.HookReg.SetStr(hooks.StoreEditTargetPath, targetPath)
	gov.HookReg.Set(hooks.StoreEditTargetWasRead, boolStore(targetPath != "" && gov.Ledger.WasRead(targetPath)))
	gov.HookReg.Set(hooks.StoreEditAnchorSufficient, boolStore(tc.Name == "edit" && toolpolicy.HasSufficientEditAnchor(tc.Args)))
	exists := false
	if targetPath != "" {
		if resolved, err := tools.ValidatePath(targetPath); err == nil {
			if _, err := os.Stat(resolved); err == nil {
				exists = true
			}
		}
	}
	gov.HookReg.Set(hooks.StoreEditTargetExists, boolStore(exists))
}

func boolStore(ok bool) int64 {
	if ok {
		return 1
	}
	return 0
}
