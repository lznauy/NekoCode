package hooks

import hookbuiltin "nekocode/bot/hooks/builtin"

func RegisterBuiltin(r *Registry) {
	for _, h := range hookbuiltin.All() {
		r.Register(adaptBuiltinHook(h))
	}
}

func quotaHook() Hook {
	return adaptBuiltinHook(hookbuiltin.QuotaHook())
}

func verificationHook() Hook {
	return adaptBuiltinHook(hookbuiltin.VerificationHook())
}

func explorationExhaustedHook() Hook {
	return adaptBuiltinHook(hookbuiltin.ExplorationExhaustedHook())
}

func explorationGuardHook() Hook {
	return adaptBuiltinHook(hookbuiltin.ExplorationGuardHook())
}

func exploreCascadeHook() Hook {
	return adaptBuiltinHook(hookbuiltin.ExploreCascadeHook())
}

func progressStallHook() Hook {
	return adaptBuiltinHook(hookbuiltin.ProgressStallHook())
}

func completionQualityHook() Hook {
	return adaptBuiltinHook(hookbuiltin.CompletionQualityHook())
}

func garbledCircuitBreaker() Hook {
	return adaptBuiltinHook(hookbuiltin.GarbledCircuitBreaker())
}

func adaptBuiltinHook(h hookbuiltin.Hook) Hook {
	return Hook{
		Name:  h.Name,
		Point: HookPoint(h.Point),
		On: func(s *Snapshot) *Result {
			return adaptBuiltinResult(h.On(builtinState{s: s}))
		},
	}
}

type builtinState struct {
	s *Snapshot
}

func (s builtinState) Get(key string) int64 {
	return s.s.get(key)
}

func (s builtinState) Set(key string, value int64) {
	s.s.set(key, value)
}

func (s builtinState) Flag(key string) bool {
	return s.s.flag(key)
}

func (s builtinState) GetStr(key string) string {
	return s.s.getStr(key)
}

func (s builtinState) ToolName() string {
	return s.s.Tool
}

func (s builtinState) ToolArgs() map[string]any {
	return s.s.Args
}

func adaptBuiltinResult(r *hookbuiltin.Result) *Result {
	if r == nil {
		return nil
	}
	out := &Result{}
	if r.Hint != nil {
		out.Hint = &Hint{Type: r.Hint.Type, Severity: r.Hint.Severity, Content: r.Hint.Content}
	}
	if r.Stop != nil {
		stop := StopReason(*r.Stop)
		out.Stop = &stop
	}
	if r.BlockTool != nil {
		out.BlockTool = &BlockTool{Tool: r.BlockTool.Tool, Reason: r.BlockTool.Reason}
	}
	if r.RequireTool != nil {
		out.RequireTool = &RequireTool{Tool: r.RequireTool.Tool, Reason: r.RequireTool.Reason}
	}
	if r.BlockFinal != nil {
		out.BlockFinal = &BlockFinal{Reason: r.BlockFinal.Reason}
	}
	if r.StatePatch != nil {
		out.StatePatch = &StatePatch{Ints: r.StatePatch.Ints}
	}
	return out
}
