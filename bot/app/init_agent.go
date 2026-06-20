package app

import (
	"context"
	"fmt"

	"nekocode/bot/agent"
	"nekocode/bot/agent/subagent"
	"nekocode/bot/app/contextguard"
	"nekocode/bot/app/taskwire"
	"nekocode/bot/config"
	"nekocode/bot/tools"
	"nekocode/common"
	"nekocode/llm"
	"nekocode/llm/types"
)

func (b *Bot) initAgent() {
	am := b.cfg.ActiveModelConfig()
	llmClient := llm.NewClientWithProtocol(am.Provider, am.APIKey, am.BaseURL, am.Model, am.Protocol)

	fm := b.cfg.ResolveModel(b.cfg.FlashModel)
	mergeClient := llm.NewClientWithProtocol(fm.Provider, fm.APIKey, fm.BaseURL, fm.Model, fm.Protocol)
	mergeClient.SetDisableThinking(true)
	mergeClient.SetMaxTokens(2000)
	b.ctxMgr.MergeClient = mergeClient

	b.ag = agent.New(context.Background(), b.ctxMgr, llmClient, b.toolRegistry)
	b.ag.SetHookRegistry(b.hookReg)

	if b.confirmFn != nil {
		b.ag.SetConfirmFn(b.confirmFn)
	}
	if b.phaseFn != nil {
		b.ag.SetPhaseFn(b.phaseFn)
	}
	if b.todoFn != nil {
		b.ag.WireTodoWrite(func(items []common.TodoItem) {
			b.ctxMgr.SetTodos(items)
			b.todoFn(items)
		})
	}

	b.ag.SetContextTransform(func(msgs []types.Message) []types.Message {
		return contextguard.ApplyToolResultGuardrail(msgs, &b.lastGuardrailWarned)
	})

	b.wireTaskTool(fm)
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
		cfg, ok := taskwire.BuildRunConfig(taskwire.RunConfigInput{
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
		return taskwire.ToTaskResult(result), err
	})
}
