package builtin

func CompletionQualityHook() Hook {
	return Hook{
		Name: "completion_quality", Point: PostTurn,
		On: func(s State) *Result {
			if s.Get(StoreStepInputLen) > 0 && s.Get(StoreStepInputLen) < 6 {
				return nil
			}
			if s.Get(StoreHasTasks) == 0 {
				s.Set(CounterQualityWarned, 0)
				return nil
			}
			if s.Get(StoreTasksAllDone) == 0 {
				s.Set(CounterQualityWarned, 0)
				return nil
			}
			if s.Flag(CounterQualityWarned) {
				return nil
			}
			if s.Get(StoreTurnToolCalls) > 0 {
				s.Set(CounterQualityWarned, 1)
				return nil
			}

			hasModifications := s.Get(StoreLedgerModified) > 0
			hasVerification := s.Get(StoreLedgerVerified) == 1
			s.Set(CounterQualityWarned, 1)

			if hasModifications && hasVerification {
				return nil
			}
			if hasModifications {
				return &Result{BlockFinal: &BlockFinal{
					Reason: "所有任务标记为完成，文件已修改但未验证。请运行验证命令确认修改正确；如果无法验证，最终回答必须明确说明未验证。",
				}}
			}
			return &Result{Hint: &Hint{Type: "quality", Severity: "info",
				Content: "所有任务标记为完成，但 ledger 中没有文件修改记录。如果这是纯分析任务，请在最终回答中明确说明无需修改文件；如果需要改代码，请继续执行修改。"}}
		},
	}
}
