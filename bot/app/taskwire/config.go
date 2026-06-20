package taskwire

import (
	"context"

	"nekocode/bot/agent/subagent"
	"nekocode/bot/tools"
	"nekocode/common"
)

type RunConfigInput struct {
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

func BuildRunConfig(input RunConfigInput) (subagent.RunConfig, bool) {
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
