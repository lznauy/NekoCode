package hooks

import "fmt"

func RegisterBuiltin(r *Registry) {
	r.Register(quotaHook())
	r.Register(verificationHook())
	r.Register(unfinishedWorkHook())
	r.Register(explorationExhaustedHook())
	r.Register(exploreCascadeHook())
	r.Register(garbledCircuitBreaker())
}

func quotaHook() Hook {
	return Hook{
		Name: "quota", Point: PreTurn,
		On: func(s *Snapshot) *Result {
			left := s.get(StoreQuotaReads)
			if left > 2 {
				s.set("gauge:last_quota_warned", 0)
				return nil
			}
			if left == s.get("gauge:last_quota_warned") {
				return nil
			}
			s.set("gauge:last_quota_warned", left)
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
		Name: "verification", Point: PreTurn,
		On: func(s *Snapshot) *Result {
			if !s.flag(StoreFileModified) {
				s.set("flag:verify_injected", 0)
				return nil
			}
			if s.flag("flag:verify_injected") {
				return nil
			}
			s.set("flag:verify_injected", 1)
			return &Result{Hint: &Hint{Type: "verification", Severity: "warning",
				Content: "你修改了文件。在报告完成之前：先 build + test 确认修改正确，然后报告结果。如果已经验证过，忽略此提示。"}}
		},
	}
}

func unfinishedWorkHook() Hook {
	return Hook{
		Name: "unfinished_work", Point: PostTurn,
		On: func(s *Snapshot) *Result {
			if s.get(StoreTasksAllDone) == 0 {
				return &Result{Hint: &Hint{Type: "verification", Severity: "critical",
					Content: "你还有未完成的任务。完成所有任务后再处理新请求——不要忽视已有任务。"}}
			}
			return nil
		},
	}
}

func explorationExhaustedHook() Hook {
	return Hook{
		Name: "exploration_exhausted", Point: PreTurn,
		On: func(s *Snapshot) *Result {
			if s.get(StoreExploreScore) > 0 {
				s.set("flag:explore_injected", 0)
				return nil
			}
			if s.flag("flag:explore_injected") {
				return nil
			}
			s.set("flag:explore_injected", 1)
			return &Result{Hint: &Hint{Type: "exploration", Severity: "critical",
				Content: "你已经探索够了。不要再调用 read/grep/glob/list——继续搜索只会浪费轮次。基于已有信息，要么编辑/写入文件，要么报告发现。\n\n你的任务：" + s.getStr(StoreStepInput)}}
		},
	}
}

func exploreCascadeHook() Hook {
	return Hook{
		Name: "explore_cascade", Point: PostTool,
		On: func(s *Snapshot) *Result {
			if s.get(StoreToolResearcher) == 0 {
				s.set("counter:cascade_researcher", 0)
				return nil
			}
			n := s.get("counter:cascade_researcher") + 1
			s.set("counter:cascade_researcher", n)
			if n < 2 {
				return nil
			}
			return &Result{Hint: &Hint{Type: "explore_cascade", Severity: "warning",
				Content: fmt.Sprintf("你已经启动了 %d 个 researcher 子 Agent 但无产出。如果已收集足够信息，立即综合发现并行动。\n\n你的任务：%s",
					n, s.getStr(StoreStepInput))}}
		},
	}
}

func garbledCircuitBreaker() Hook {
	return Hook{
		Name: "garbled_circuit_breaker", Point: PostTurn,
		On: func(s *Snapshot) *Result {
			if s.get(StoreRespGarbled) >= 3 {
				stop := StopFormatError
				return &Result{Stop: &stop}
			}
			return nil
		},
	}
}
