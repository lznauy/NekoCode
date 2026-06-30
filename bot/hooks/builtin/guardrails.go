package builtin

import "fmt"

const (
	toolResultThreshold = 40
	toolResultInterval  = 10
)

func ToolResultGuardrailHook() Hook {
	return Hook{
		Name: "tool_result_guardrail", Point: PreModelRequest,
		On: func(s State) *Result {
			count := s.Get(StoreToolResultCount)
			lastWarned := s.Get(CounterToolResultWarned)
			if count <= toolResultThreshold || count-lastWarned < toolResultInterval {
				return nil
			}
			s.Set(CounterToolResultWarned, count)
			return &Result{Hint: &Hint{
				Type:     "tool_results",
				Severity: "warning",
				Content:  fmt.Sprintf("%d tool results accumulated. Check for unfinished sub-tasks - if any, continue with task. If all done, call task(verify) to validate, then report results.", count),
			}}
		},
	}
}

func ReadBeforeWriteHook() Hook {
	return Hook{
		Name: "read_before_write", Point: PreToolUse,
		On: func(s State) *Result {
			name := s.ToolName()
			if name != "edit" && name != "write" {
				return nil
			}
			path := s.GetStr(StoreEditTargetPath)
			if path == "" || s.Get(StoreEditTargetExists) != 1 || s.Get(StoreEditTargetWasRead) == 1 {
				return nil
			}
			if name == "edit" && s.Get(StoreEditAnchorSufficient) == 1 {
				return nil
			}
			return &Result{BlockTool: &BlockTool{
				Tool:   name,
				Reason: "你正在修改 " + path + "，但 ledger 中没有该文件的读取记录。请先 Read 确认当前内容，确认差异后再 edit/write。",
			}}
		},
	}
}

func ReadOnlySpiralHook() Hook {
	return Hook{
		Name: "read_only_spiral", Point: PostTool,
		On: func(s State) *Result {
			if s.Get(StoreReadOnlyStreak) < 3 {
				return nil
			}
			s.Set(StoreReadOnlyStreak, 0)
			return &Result{Hint: &Hint{
				Type:     "read_only_spiral",
				Severity: "warning",
				Content:  "You've been reading without acting. Summarize your findings now - don't read any more files.",
			}}
		},
	}
}
