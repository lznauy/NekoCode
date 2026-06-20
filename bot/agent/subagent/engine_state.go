package subagent

import (
	"time"

	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/tools"
)

type runState struct {
	startTime      time.Time
	toolUseCount   int
	totalTokens    int
	sensitiveOps   int
	readOnlyStreak int
	lastText       string
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

func (s *runState) recordCalls(calls []tools.ToolCallItem) {
	s.toolUseCount += len(calls)
	for _, c := range calls {
		if isSensitiveCall(c) {
			s.sensitiveOps++
		}
	}
}
