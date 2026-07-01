package runtime

import (
	"context"
	"strings"

	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/hooks"
	"nekocode/bot/llm/types"
	aggov "nekocode/bot/policy"
	"nekocode/bot/policy/budget"
	"nekocode/bot/tools/core"
	"nekocode/bot/tools"
)

func newTestAgent() *Agent {
	ctxMgr := ctxmgr.NewSub("test", 128000, nil)
	reg := tools.NewRegistry()
	a := New(context.Background(), ctxMgr, nil, reg)
	a.deps.gov = aggov.NewManager(hooks.NewRegistry())
	hooks.RegisterBuiltin(a.deps.gov.HookReg)
	return a
}

func preToolBlockReasonForTest(a *Agent, tc core.ToolCallItem) string {
	filtered := a.toolRunner.FilterToolCalls([]core.ToolCallItem{tc}, &budget.ToolQuota{MaxSlots: 8})
	return filtered.Blocked[0]
}

func messagesContain(msgs []types.Message, substr string) bool {
	for _, msg := range msgs {
		if strings.Contains(msg.Content, substr) {
			return true
		}
	}
	return false
}
