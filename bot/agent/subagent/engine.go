package subagent

import (
	"context"
	"time"

	agentruntime "nekocode/bot/agent/runtime"
	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/common/debug"
	"nekocode/bot/llm/types"
	"nekocode/bot/tools"
	"nekocode/bot/tools/core"
	"nekocode/bot/tools/runner"
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

type runState struct {
	startTime      time.Time
	toolUseCount   int
	totalTokens    int
	sensitiveOps   int
	readOnlyStreak int
	lastText       string
}

func NewEngine(llmClient types.LLM, registry *tools.Registry, mergeClient types.LLM) *Engine {
	return &Engine{llmClient: llmClient, toolRegistry: registry, mergeClient: mergeClient}
}

func newRunState() *runState {
	return &runState{startTime: time.Now()}
}

func (s *runState) addTokens(cfg RunConfig) func(int, int) {
	return func(prompt, compl int) {
		s.totalTokens += prompt + compl
		if cfg.AddTokens != nil {
			cfg.AddTokens(prompt, compl)
		}
	}
}

func (s *runState) meta(ctxMgr *ctxmgr.Manager) runMeta {
	hit, miss := ctxMgr.Tracker.CacheStats()
	return runMeta{
		totalTokens:     s.totalTokens,
		toolUseCount:    s.toolUseCount,
		durationMs:      time.Since(s.startTime).Milliseconds(),
		cacheHitTokens:  hit,
		cacheMissTokens: miss,
		sensitiveOps:    s.sensitiveOps,
	}
}

func (s *runState) recordCalls(calls []core.ToolCallItem) {
	s.toolUseCount += len(calls)
	for _, c := range calls {
		if isSensitiveCall(c) {
			s.sensitiveOps++
		}
	}
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

	ctxMgr.Add("user", cfg.Prompt)
	phase := phaseReporter(cfg)
	phase("Waiting")

	run := &engineRun{
		engine:   e,
		ctx:      ctx,
		cfg:      cfg,
		ctxMgr:   ctxMgr,
		executor: executor,
		state:    state,
		phase:    phase,
		log:      subLog,
	}

	agentruntime.RunLoop(agentruntime.Loop{
		StepLimitReached: run.stepLimitReached,
		Step:             run.stepOnce,
	})

	return run.finish()
}

type engineRun struct {
	engine   *Engine
	ctx      context.Context
	cfg      RunConfig
	ctxMgr   *ctxmgr.Manager
	executor *runner.Executor
	state    *runState
	phase    func(string)
	log      func(string, ...any)

	step   int
	result *Result
	err    error
}

func (r *engineRun) stepLimitReached() bool {
	if r.step < maxSubAgentSteps {
		return false
	}
	r.log("max steps reached: step=%d", r.step)
	r.result = buildPartialResult(r.state.lastText, r.state.meta(r.ctxMgr))
	return true
}

func (r *engineRun) stepOnce() bool {
	if r.ctx.Err() != nil {
		r.log("interrupted: step=%d lastText=%q", r.step, r.state.lastText[:min(len(r.state.lastText), 200)])
		r.result = buildPartialResult(r.state.lastText, r.state.meta(r.ctxMgr))
		r.err = r.ctx.Err()
		return true
	}

	r.ctxMgr.AutoCompactIfNeeded()
	calls, text, err := r.engine.reason(r.ctx, r.ctxMgr, r.cfg.AgentType.Tools, r.state.addTokens(r.cfg), r.phase)
	if err != nil {
		r.log("error: %v", err)
		if r.state.lastText != "" {
			r.log("partial_result: %q", r.state.lastText[:min(len(r.state.lastText), 300)])
			r.result = buildPartialResult(r.state.lastText, r.state.meta(r.ctxMgr))
			return true
		}
		r.result = buildFailedResult(err.Error(), r.state.meta(r.ctxMgr))
		r.err = err
		return true
	}

	if text != "" {
		r.state.lastText = text
	}
	if len(calls) == 0 {
		r.complete(text)
		return true
	}

	r.state.recordCalls(calls)
	r.engine.executeToolBatch(r.ctx, r.cfg, r.ctxMgr, r.executor, calls, r.state, r.phase, r.log)
	r.phase("Waiting")
	r.step++
	return false
}

func (r *engineRun) complete(text string) {
	r.phase("done")
	r.result = buildResult(text, r.state.meta(r.ctxMgr))
	r.log("result: tokens=%d tools=%d duration=%dms output=%q",
		r.result.TotalTokens, r.result.ToolUseCount, r.result.DurationMs,
		text[:min(len(text), 300)])
}

func (r *engineRun) finish() (*Result, error) {
	if r.result == nil {
		r.result = buildPartialResult(r.state.lastText, r.state.meta(r.ctxMgr))
	}
	return r.result, r.err
}
