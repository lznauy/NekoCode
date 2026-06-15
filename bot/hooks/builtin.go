package hooks

import "fmt"

func RegisterBuiltin(r *Registry) {
	r.Register(quotaHook())
	r.Register(verificationHook())
	r.Register(explorationExhaustedHook())
	r.Register(exploreCascadeHook())
	r.Register(toolIdleHook())
	r.Register(completionQualityHook())
	r.Register(garbledCircuitBreaker())
}

func quotaHook() Hook {
	return Hook{
		Name: "quota", Point: PreTurn,
		On: func(s *Snapshot) *Result {
			left := s.get(StoreQuotaReads)
			if left > 2 {
				s.set("flag:quota_warned", 0)
				return nil
			}
			if left == s.get("flag:quota_warned") {
				return nil
			}
			s.set("flag:quota_warned", left)
			sev := "warning"
			content := fmt.Sprintf("剩余 %d 次读取配额。请使用已有信息，优先进行实质性修改。", left)
			if left <= 0 {
				sev = "critical"
				content = "读取配额已耗尽。不要再尝试 read/grep/glob——基于已有信息行动。"
			}
			return &Result{Hint: &Hint{Type: "quota", Severity: sev, Content: content}}
		},
	}
}

func verificationHook() Hook {
	return Hook{
		Name: "verification", Point: PostTurn,
		On: func(s *Snapshot) *Result {
			// Only fire when: has tasks, not all done, and no tool calls this turn.
			if s.get(StoreHasTasks) == 0 || s.get(StoreTasksAllDone) == 1 {
				s.set("flag:verify_injected", 0)
				return nil
			}
			if s.get(StoreTurnToolCalls) > 0 {
				return nil
			}
			if s.flag("flag:verify_injected") {
				return nil
			}
			s.set("flag:verify_injected", 1)
			return &Result{Hint: &Hint{Type: "verification", Severity: "warning",
				Content: "你还有未完成的任务，但本轮没有调用任何工具。请继续完成任务，或报告当前进度。"}}
		},
	}
}

func explorationExhaustedHook() Hook {
	return Hook{
		Name: "exploration_exhausted", Point: PreTurn,
		On: func(s *Snapshot) *Result {
			// Only fire after significant exploration (>= 10 calls).
			if s.get(StoreExploreCalls) < 10 {
				s.set("flag:explore_injected", 0)
				return nil
			}
			if s.get(StoreExploreScore) > 0 {
				s.set("flag:explore_injected", 0)
				return nil
			}
			if s.flag("flag:explore_injected") {
				return nil
			}
			s.set("flag:explore_injected", 1)
			return &Result{Hint: &Hint{Type: "exploration", Severity: "warning",
				Content: "你已经探索够了。不要再调用 read/grep/glob/list——继续搜索只会浪费轮次。基于已有信息，要么编辑/写入文件，要么报告发现。\n\n你的任务：" + s.getStr(StoreStepInput)}}
		},
	}
}

func exploreCascadeHook() Hook {
	return Hook{
		Name: "explore_cascade", Point: PostTool,
		On: func(s *Snapshot) *Result {
			// StoreToolResearcher counts researchers launched this turn.
			n := s.get(StoreToolResearcher)
			if n < 4 {
				return nil
			}
			return &Result{Hint: &Hint{Type: "explore_cascade", Severity: "warning",
				Content: fmt.Sprintf("你已经启动了 %d 个 researcher 子 Agent。如果已收集足够信息，立即综合发现并行动。\n\n你的任务：%s",
					n, s.getStr(StoreStepInput))}}
		},
	}
}

func toolIdleHook() Hook {
	return Hook{
		Name: "tool_idle", Point: PostTool,
		On: func(s *Snapshot) *Result {
			// If this turn made substantive changes, reset idle counter.
			if s.get(StoreHasEdits) == 1 {
				s.set("counter:idle_calls", 0)
				s.set("flag:idle_warned", 0)
				return nil
			}
			// Only count turns that actually used tools.
			turnCalls := s.get(StoreTurnToolCalls)
			if turnCalls == 0 {
				return nil
			}
			n := s.get("counter:idle_calls") + turnCalls
			s.set("counter:idle_calls", n)
			if n >= 50 {
				if s.flag("flag:idle_warned") {
					return nil
				}
				s.set("flag:idle_warned", 1)
				return &Result{Hint: &Hint{Type: "idle", Severity: "warning",
					Content: fmt.Sprintf("你已经连续 %d 次只使用只读工具（read/grep/glob/list/tree）。基于已有信息，开始写代码或编辑文件推进任务。\n\n你的任务：%s",
						n, s.getStr(StoreStepInput))}}
			}
			return nil
		},
	}
}

func completionQualityHook() Hook {
	return Hook{
		Name: "completion_quality", Point: PostTurn,
		On: func(s *Snapshot) *Result {
			// Trivial input (e.g. "你好", "hello") → don't quality-check.
			if s.get(StoreStepInputLen) > 0 && s.get(StoreStepInputLen) < 6 {
				return nil
			}
			if s.get(StoreHasTasks) == 0 {
				s.set("flag:quality_warned", 0)
				return nil
			}
			if s.get(StoreTasksAllDone) == 0 {
				s.set("flag:quality_warned", 0)
				return nil
			}
			if s.flag("flag:quality_warned") {
				return nil
			}
			// Skip if this turn had tool calls (may be pure analysis).
			if s.get(StoreTurnToolCalls) > 0 {
				s.set("flag:quality_warned", 1)
				return nil
			}
			if s.flag(StoreFileModified) {
				s.set("flag:quality_warned", 1)
				return nil
			}
			s.set("flag:quality_warned", 1)
			return &Result{Hint: &Hint{Type: "quality", Severity: "warning",
				Content: "所有任务标记为完成，但此轮未修改任何文件。如果实际工作未完成，请继续；如果确实无需修改文件（如纯分析任务），可忽略此提示。"}}
		},
	}
}

func garbledCircuitBreaker() Hook {
	return Hook{
		Name: "garbled_circuit_breaker", Point: PostTurn,
		On: func(s *Snapshot) *Result {
			if s.get(StoreRespGarbled) >= 5 {
				stop := StopFormatError
				return &Result{Stop: &stop}
			}
			return nil
		},
	}
}
