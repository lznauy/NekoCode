package app

import (
	"context"
	"fmt"

	"nekocode/bot/agent/runtime"
	"nekocode/bot/agent/subagent"
	"nekocode/bot/config"
	"nekocode/bot/contextmgr"
	"nekocode/bot/llm"
	"nekocode/bot/tools"
)

type subagentWiring struct {
	toolRegistry  *tools.Registry
	ctxMgr        *contextmgr.Manager
	cwd           string
	projCtx       string
	contextWindow int
	getAgent      func() *runtime.Agent
}

type subagentWiringDeps struct {
	ToolRegistry  *tools.Registry
	CtxMgr        *contextmgr.Manager
	CWD           string
	ProjectCtx    string
	ContextWindow int
	GetAgent      func() *runtime.Agent
}

func (w *subagentWiring) Init(d subagentWiringDeps) {
	w.toolRegistry = d.ToolRegistry
	w.ctxMgr = d.CtxMgr
	w.cwd = d.CWD
	w.projCtx = d.ProjectCtx
	w.contextWindow = d.ContextWindow
	w.getAgent = d.GetAgent
}

func (w *subagentWiring) WireTaskTool(fm config.ModelConfig) {
	subLLM := llm.NewClientWithProtocol(fm.Provider, fm.APIKey, fm.BaseURL, fm.Model, fm.Protocol)
	engine := subagent.NewEngine(subLLM, w.toolRegistry, w.ctxMgr.MergeClient)

	t, err := w.toolRegistry.Get("task")
	if err != nil {
		return
	}
	taskTool, ok := t.(tools.TaskRunnerTool)
	if !ok {
		return
	}
	taskTool.Wire(func(ctx context.Context, prompt, agentType, thoroughness string) (*tools.TaskResult, error) {
		cfg, ok := w.buildSubagentRunConfig(ctx, prompt, agentType, thoroughness)
		if !ok {
			return nil, fmt.Errorf("unknown sub-agent type: %s", agentType)
		}
		result, err := engine.Run(ctx, cfg)
		if result != nil && (result.CacheHitTokens > 0 || result.CacheMissTokens > 0) {
			w.ctxMgr.Tracker.RecordSubagent(result.TotalTokens, result.CacheHitTokens, result.CacheMissTokens)
		}
		return subagentTaskResult(result), err
	})
}

func (w *subagentWiring) buildSubagentRunConfig(ctx context.Context, prompt, agentType, thoroughness string) (subagent.RunConfig, bool) {
	ag := w.getAgent()
	return buildSubagentRunConfig(subagentRunConfigInput{
		Context:        ctx,
		Prompt:         prompt,
		AgentTypeName:  agentType,
		Thoroughness:   thoroughness,
		Cwd:            w.cwd,
		ProjectContext: w.projCtx,
		ContextWindow:  w.contextWindow,
		ConfirmFn:      ag.ConfirmFn(),
		ToolState:      ag.ToolExecutionState(),
		PhaseFn:        ag.PhaseFn(),
		AddTokens:      ag.AddTokens,
	})
}
