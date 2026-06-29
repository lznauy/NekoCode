package subagent

import (
	"context"
	"time"

	"nekocode/bot/debug"
	"nekocode/bot/llm/types"
	"nekocode/bot/tools"
)

const (
	thoroughDeep     = "very thorough"
	taskToolName     = "task"
	maxSubAgentSteps = 50
)

type Engine struct {
	llmClient    types.LLM
	toolRegistry *tools.Registry
	mergeClient  types.LLM
}

func NewEngine(llmClient types.LLM, registry *tools.Registry, mergeClient types.LLM) *Engine {
	return &Engine{llmClient: llmClient, toolRegistry: registry, mergeClient: mergeClient}
}

func (e *Engine) Run(ctx context.Context, cfg RunConfig) (*Result, error) {
	subLog := debug.Sub(cfg.AgentType.Name)
	subLog("start: prompt=%q", cfg.Prompt[:min(len(cfg.Prompt), 120)])
	defer func(start time.Time) {
		subLog("done: duration=%v", time.Since(start).Round(time.Millisecond))
	}(time.Now())

	state := newRunState()
	executor, cleanupExecutor := e.newExecutor(cfg)
	defer cleanupExecutor()

	ctxMgr := e.newContextManager(cfg)
	restoreThinking := e.applyThinkingMode(cfg)
	defer restoreThinking()

	ctxMgr.Add("user", cfg.Prompt)
	phase := phaseReporter(cfg)
	phase("Waiting")

	for step := 0; ; step++ {
		select {
		case <-ctx.Done():
			subLog("interrupted: step=%d lastText=%q", step, state.lastText[:min(len(state.lastText), 200)])
			return buildPartialResult(state.lastText, state.meta(ctxMgr)), ctx.Err()
		default:
		}

		if step >= maxSubAgentSteps {
			subLog("max steps reached: step=%d", step)
			return buildPartialResult(state.lastText, state.meta(ctxMgr)), nil
		}

		ctxMgr.AutoCompactIfNeeded()
		calls, text, err := e.reason(ctx, ctxMgr, cfg.AgentType.Tools, state.addTokens(cfg), phase)
		if err != nil {
			subLog("error: %v", err)
			if state.lastText != "" {
				subLog("partial_result: %q", state.lastText[:min(len(state.lastText), 300)])
				return buildPartialResult(state.lastText, state.meta(ctxMgr)), nil
			}
			return buildFailedResult(err.Error(), state.meta(ctxMgr)), err
		}

		if text != "" {
			state.lastText = text
		}
		if len(calls) == 0 {
			phase("done")
			result := buildResult(text, state.meta(ctxMgr))
			subLog("result: tokens=%d tools=%d duration=%dms output=%q",
				result.TotalTokens, result.ToolUseCount, result.DurationMs,
				text[:min(len(text), 300)])
			return result, nil
		}

		state.recordCalls(calls)
		e.executeToolBatch(ctx, cfg, ctxMgr, executor, calls, state, phase, subLog)
		phase("Waiting")
	}
}
