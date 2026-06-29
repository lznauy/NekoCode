package app

import (
	"context"
	"fmt"

	"nekocode/bot/agent/subagent"
	"nekocode/bot/config"
	"nekocode/bot/llm"
	"nekocode/bot/tools"
	"nekocode/common"
)

type subagentRunConfigInput struct {
	Context        context.Context
	Prompt         string
	AgentTypeName  string
	Thoroughness   string
	Cwd            string
	ProjectContext string
	ContextWindow  int
	ConfirmFn      common.ConfirmFunc
	ToolState      *tools.ExecutionState
	PhaseFn        func(string)
	AddTokens      func(prompt, completion int)
}

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
		cfg, ok := b.buildSubagentRunConfig(ctx, prompt, agentType, thoroughness)
		if !ok {
			return nil, fmt.Errorf("unknown sub-agent type: %s", agentType)
		}
		result, err := engine.Run(ctx, cfg)
		if result != nil && (result.CacheHitTokens > 0 || result.CacheMissTokens > 0) {
			b.ctxMgr.Tracker.RecordSubagent(result.TotalTokens, result.CacheHitTokens, result.CacheMissTokens)
		}
		return subagentTaskResult(result), err
	})
}

func (b *Bot) buildSubagentRunConfig(ctx context.Context, prompt, agentType, thoroughness string) (subagent.RunConfig, bool) {
	return buildSubagentRunConfig(subagentRunConfigInput{
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
}

func buildSubagentRunConfig(input subagentRunConfigInput) (subagent.RunConfig, bool) {
	at, ok := subagent.Get(input.AgentTypeName)
	if !ok {
		return subagent.RunConfig{}, false
	}
	cfg := subagent.RunConfig{
		Prompt:          input.Prompt,
		AgentType:       at,
		Cwd:             input.Cwd,
		ProjectContext:  input.ProjectContext,
		Thoroughness:    input.Thoroughness,
		ContextWindow:   input.ContextWindow,
		DisableThinking: true,
		ConfirmFn:       input.ConfirmFn,
		ToolState:       input.ToolState,
		AddTokens:       input.AddTokens,
	}
	if subCB, ok := tools.TaskCallbackFromCtx(input.Context); ok {
		cfg.OnToolCall = func(ev subagent.ToolCallEvent) {
			subCB("sub_"+ev.Action, ev.ToolName, ev.ToolArgs, ev.Output)
		}
	}
	if input.PhaseFn != nil {
		cfg.OnPhase = func(p string) { input.PhaseFn(at.Name + " · " + p) }
	}
	return cfg, true
}

func subagentTaskResult(result *subagent.Result) *tools.TaskResult {
	if result == nil {
		return nil
	}
	status := tools.TaskStatusCompleted
	switch result.Status {
	case subagent.StatusFailed:
		status = tools.TaskStatusFailed
	case subagent.StatusPartial:
		status = tools.TaskStatusPartial
	}
	return &tools.TaskResult{
		Status:  status,
		Content: subagent.FormatResult(result),
	}
}
