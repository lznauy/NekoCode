package subagent

import (
	"context"

	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/governance"
	"nekocode/bot/llm/types"
	"nekocode/bot/tools"
	"nekocode/common"
)

func (e *Engine) newExecutor(cfg RunConfig) (*tools.Executor, func()) {
	executor := tools.NewExecutor(e.toolRegistry)
	executor.SetConfirmFn(func(req common.ConfirmRequest) bool {
		return req.Level < common.LevelWrite
	})
	if cfg.ConfirmFn != nil {
		executor.SetConfirmFn(cfg.ConfirmFn)
	}

	toolState := executor.ExecutionState()
	if cfg.ToolState != nil {
		toolState.FileCache.Seed(cfg.ToolState.FileCache)
		if cfg.ToolState.SnapshotStore != nil {
			toolState.SnapshotStore = cfg.ToolState.SnapshotStore
		}
	}
	return executor, func() {
		if cfg.ToolState != nil && cfg.ToolState.FileCache != nil {
			cfg.ToolState.FileCache.Merge(toolState.FileCache)
		}
	}
}

func (e *Engine) executeToolBatch(ctx context.Context, cfg RunConfig, ctxMgr *ctxmgr.Manager, executor *tools.Executor, calls []tools.ToolCallItem, state *runState, phase func(string), subLog func(string, ...any)) {
	var toolNames []string
	for _, c := range calls {
		toolNames = append(toolNames, c.Name)
		phase("Running " + c.Name)
		if cfg.OnToolCall != nil {
			cfg.OnToolCall(ToolCallEvent{
				Action:   "tool_start",
				ToolName: c.Name,
				ToolArgs: tools.FormatArgs(c.Args),
			})
		}
	}

	subLog("tools: %v", toolNames)
	results := executor.ExecuteBatch(ctx, calls)
	batch := make([]ctxmgr.ToolResultMsg, len(results))
	for i, r := range results {
		content := r.EffectiveOutput()
		batch[i] = ctxmgr.ToolResultMsg{
			Message:  types.Message{Content: content, ToolCallID: r.ID},
			ToolName: calls[i].Name,
		}
		if cfg.OnToolCall != nil {
			cfg.OnToolCall(ToolCallEvent{
				Action: "execute_tool", ToolName: calls[i].Name,
				ToolArgs: tools.FormatArgs(calls[i].Args), Output: content,
			})
		}
	}
	ctxMgr.AddToolResultsBatch(batch)
	applyReadOnlySpiralGuard(ctxMgr, calls, state)
}

func applyReadOnlySpiralGuard(ctxMgr *ctxmgr.Manager, calls []tools.ToolCallItem, state *runState) {
	if tools.IsAllExploratory(calls) {
		state.readOnlyStreak++
		if state.readOnlyStreak >= 3 {
			ctxMgr.Add("user", governance.GuardReadOnlySpiral, "system")
			state.readOnlyStreak = 0
		}
		return
	}
	state.readOnlyStreak = 0
}
