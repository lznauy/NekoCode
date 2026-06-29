package app

import (
	"context"
	"fmt"

	"nekocode/bot/agent/subagent"
	"nekocode/bot/config"
	"nekocode/bot/llm"
	"nekocode/bot/tools"
	"nekocode/bot/tools/tasktool"
)

func (b *Bot) wireTaskTool(fm config.ModelConfig) {
	subLLM := llm.NewClientWithProtocol(fm.Provider, fm.APIKey, fm.BaseURL, fm.Model, fm.Protocol)
	engine := subagent.NewEngine(subLLM, b.toolRegistry, b.ctxMgr.MergeClient)

	t, err := b.toolRegistry.Get("task")
	if err != nil {
		return
	}
	taskTool, ok := t.(tools.TaskRunnerTool)
	if !ok {
		return
	}
	taskTool.Wire(func(ctx context.Context, prompt, agentType, thoroughness string) (*tools.TaskResult, error) {
		cfg, ok := tasktool.BuildRunConfig(tasktool.RunConfigInput{
			Context:        ctx,
			Prompt:         prompt,
			AgentTypeName:  agentType,
			Thoroughness:   thoroughness,
			Cwd:            b.cwd,
			ProjectContext: b.projCtx,
			ContextWindow:  b.cfg.ContextWindow,
			ConfirmFn:      b.ag.ConfirmFn(),
			ToolState:      b.ag.ToolExecutionState(),
			PhaseFn:        b.ag.PhaseFn(),
			AddTokens:      b.ag.AddTokens,
		})
		if !ok {
			return nil, fmt.Errorf("unknown sub-agent type: %s", agentType)
		}
		result, err := engine.Run(ctx, cfg)
		if result != nil && (result.CacheHitTokens > 0 || result.CacheMissTokens > 0) {
			b.ctxMgr.Tracker.RecordSubagent(result.TotalTokens, result.CacheHitTokens, result.CacheMissTokens)
		}
		return tasktool.ToTaskResult(result), err
	})
}
