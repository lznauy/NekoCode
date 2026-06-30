package builtin

import (
	"fmt"

	"nekocode/bot/policy/semantics"
)

func ExplorationExhaustedHook() Hook {
	return Hook{
		Name: "exploration_exhausted", Point: PreTurn,
		On: func(s State) *Result {
			if s.Get(StoreExploreCalls) < 10 {
				s.Set(CounterExploreInjected, 0)
				s.Set(PolicyExploreExhausted, 0)
				return nil
			}
			if s.Get(StoreExploreScore) > 0 {
				s.Set(CounterExploreInjected, 0)
				s.Set(PolicyExploreExhausted, 0)
				return nil
			}
			if s.Flag(CounterExploreInjected) {
				return nil
			}
			s.Set(CounterExploreInjected, 1)
			return &Result{
				Hint: &Hint{Type: "exploration", Severity: "warning",
					Content: "你已经探索够了。不要再调用探索类工具。基于已有信息，要么编辑/写入文件，要么报告发现。\n\n你的任务：" + s.GetStr(StoreStepInput)},
				RequireTool: &RequireTool{
					Tool:   "edit/write/bash",
					Reason: "探索预算已耗尽。下一步必须推进实际修改、验证，或明确报告无法继续。",
				},
				StatePatch: &StatePatch{
					Ints: map[string]int64{PolicyExploreExhausted: 1},
				},
			}
		},
	}
}

func ExplorationGuardHook() Hook {
	return Hook{
		Name: "exploration_guard", Point: PreToolUse,
		On: func(s State) *Result {
			if s.Get(PolicyExploreExhausted) != 1 {
				return nil
			}
			if !isExploratoryCall(s.ToolName(), s.ToolArgs()) {
				return nil
			}
			return &Result{BlockTool: &BlockTool{
				Tool:   s.ToolName(),
				Reason: "探索配额已耗尽。请基于已有信息使用 edit/write 进行实质性修改，或明确报告无法继续。",
			}}
		},
	}
}

func isExploratoryCall(name string, args map[string]any) bool {
	if args == nil {
		switch name {
		case "bash", "task":
			return true
		}
	}
	return semantics.ClassifyToolCall(name, args).Exploratory
}

func ExploreCascadeHook() Hook {
	return Hook{
		Name: "explore_cascade", Point: PostTool,
		On: func(s State) *Result {
			n := s.Get(StoreToolResearcher)
			if n < 4 {
				return nil
			}
			return &Result{Hint: &Hint{Type: "explore_cascade", Severity: "warning",
				Content: fmt.Sprintf("你已经启动了 %d 个 researcher 子 Agent。如果已收集足够信息，立即综合发现并行动。\n\n你的任务：%s",
					n, s.GetStr(StoreStepInput))}}
		},
	}
}
