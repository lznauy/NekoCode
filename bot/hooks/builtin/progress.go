package builtin

import "fmt"

func ProgressStallHook() Hook {
	return Hook{
		Name: "progress_stall", Point: PostTool,
		On: func(s State) *Result {
			if s.Get(StoreTurnToolCalls) == 0 {
				return nil
			}
			if s.Get(StoreHasEdits) == 1 || s.Get(StoreLedgerProgress) == 1 {
				s.Set(CounterStallTurns, 0)
				return nil
			}

			n := s.Get(CounterStallTurns) + 1
			s.Set(CounterStallTurns, n)
			if n < 8 {
				return nil
			}

			s.Set(CounterStallTurns, 0)
			return &Result{
				Hint: &Hint{Type: "stall", Severity: "warning",
					Content: fmt.Sprintf("连续 %d 轮工具调用没有产生新证据（新文件读取、修改或验证）。基于已有信息推进实际工作，或明确报告阻塞。\n\n你的任务：%s",
						n, s.GetStr(StoreStepInput))},
				RequireTool: &RequireTool{
					Tool:   "edit/write/bash",
					Reason: "连续多轮没有产生新证据。下一步必须推进实际修改、运行验证，或明确报告阻塞。",
				},
			}
		},
	}
}
