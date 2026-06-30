package runner

import (
	"context"
	"fmt"

	"nekocode/bot/tools/core"
	"nekocode/bot/tools/execution"
	"nekocode/common"
)

func (e *Executor) executeOne(ctx context.Context, tc core.ToolCallItem) core.ToolCallResult {
	tool, err := e.registry.Get(tc.Name)
	if err != nil {
		return core.ToolCallResult{ID: tc.ID, Name: tc.Name, Error: err.Error()}
	}

	level := tool.DangerLevel(tc.Args)
	if level == common.LevelForbidden {
		return core.ToolCallResult{ID: tc.ID, Name: tc.Name, Error: "forbidden: " + tc.Name}
	}

	phaseFn, confirmFn, planMode := e.callbacks()
	if phaseFn != nil {
		phaseFn(common.PhaseRunning + " " + tc.Name)
	}
	if planMode && level >= common.LevelWrite {
		return core.ToolCallResult{ID: tc.ID, Name: tc.Name, Error: "plan mode: blocked"}
	}
	if level >= common.LevelWrite && confirmFn != nil && !confirmFn(common.NewConfirmRequest(tc.Name, confirmArgs(tc.Name, tc.Args), level)) {
		return core.ToolCallResult{ID: tc.ID, Name: tc.Name, Error: "cancelled"}
	}

	paths := toolPaths(tc)
	output, execErr := e.callTool(ctx, tool, tc)
	if execErr != nil {
		return core.ToolCallResult{ID: tc.ID, Name: tc.Name, Error: execErr.Error()}
	}

	e.invalidateMutatedPaths(tc.Name, paths)
	return core.ToolCallResult{ID: tc.ID, Name: tc.Name, Output: formatOutput(tc.Name, output)}
}

func (e *Executor) callbacks() (common.PhaseFunc, common.ConfirmFunc, bool) {
	e.fnMu.RLock()
	defer e.fnMu.RUnlock()
	return e.phaseFn, e.confirmFn, e.planMode
}

func (e *Executor) callTool(ctx context.Context, tool core.Tool, tc core.ToolCallItem) (output string, execErr error) {
	defer func() {
		if r := recover(); r != nil {
			execErr = fmt.Errorf("panic: %v", r)
		}
	}()
	return tool.Execute(execution.WithExecutionState(ctx, e.state), tc.Args)
}

func (e *Executor) invalidateMutatedPaths(toolName string, paths []string) {
	if toolName != "write" && toolName != "edit" {
		return
	}
	for _, p := range paths {
		if resolved, err := validatePath(p); err == nil {
			if cache := e.state.FileCache; cache != nil {
				cache.Invalidate(resolved)
			}
		}
	}
}
