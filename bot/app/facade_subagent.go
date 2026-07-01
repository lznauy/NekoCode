package app

import (
	"context"
	"fmt"

	"nekocode/bot/agent/subagent"
	"nekocode/bot/config"
	"nekocode/bot/contextmgr"
	"nekocode/bot/llm"
	"nekocode/bot/tools"
	"nekocode/common"
)

type subagentWiring struct {
	toolRegistry  *tools.Registry
	ctxMgr        *contextmgr.Manager
	cwd           string
	projCtx       string
	contextWindow int
}

func newSubagentWiring(toolRegistry *tools.Registry, ctxMgr *contextmgr.Manager, cwd, projCtx string, contextWindow int) *subagentWiring {
	return &subagentWiring{
		toolRegistry:  toolRegistry,
		ctxMgr:        ctxMgr,
		cwd:           cwd,
		projCtx:       projCtx,
		contextWindow: contextWindow,
	}
}

func (w *subagentWiring) WireTaskTool(fm config.ModelConfig, ag agentCallbacks) {
	t, err := w.toolRegistry.Get("task")
	if err != nil {
		return
	}
	taskTool, ok := t.(tools.TaskRunnerTool)
	if !ok {
		return
	}
	taskTool.Wire(func(ctx context.Context, prompt, agentType, thoroughness string) (*tools.TaskResult, error) {
		subLLM := llm.NewClientWithProtocol(fm.Provider, fm.APIKey, fm.BaseURL, fm.Model, fm.Protocol)
		subLLM.SetDisableThinking(true)
		engine := subagent.NewEngine(subLLM, w.toolRegistry, w.ctxMgr.MergeClient)
		cfg, ok := w.buildSubagentRunConfig(ctx, prompt, agentType, thoroughness, ag)
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

type agentCallbacks interface {
	ConfirmFn() common.ConfirmFunc
	ToolExecutionState() *tools.ExecutionState
	PhaseFn() common.PhaseFunc
	AddTokens(prompt, completion int)
}

func (w *subagentWiring) buildSubagentRunConfig(ctx context.Context, prompt, agentType, thoroughness string, ag agentCallbacks) (subagent.RunConfig, bool) {
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

func buildSubagentRunConfig(input subagentRunConfigInput) (subagent.RunConfig, bool) {
	at, ok := subagent.Get(input.AgentTypeName)
	if !ok {
		return subagent.RunConfig{}, false
	}
	cfg := subagent.RunConfig{
		Prompt:         input.Prompt,
		AgentType:      at,
		Cwd:            input.Cwd,
		ProjectContext: input.ProjectContext,
		Thoroughness:   input.Thoroughness,
		ContextWindow:  input.ContextWindow,
		ConfirmFn:      input.ConfirmFn,
		ToolState:      input.ToolState,
		AddTokens:      input.AddTokens,
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
