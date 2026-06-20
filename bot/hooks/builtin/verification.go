package builtin

func VerificationHook() Hook {
	return Hook{
		Name: "verification", Point: PostTurn,
		On: func(s State) *Result {
			if s.Get(StoreHasTasks) == 0 || s.Get(StoreTasksAllDone) == 1 {
				s.Set(CounterVerifyInjected, 0)
				return nil
			}
			if s.Get(StoreTurnToolCalls) > 0 {
				return nil
			}
			if s.Flag(CounterVerifyInjected) {
				return nil
			}
			s.Set(CounterVerifyInjected, 1)
			return &Result{BlockFinal: &BlockFinal{
				Reason: "你还有未完成的任务，但本轮没有调用任何工具。请继续完成任务；如果只能报告进度，必须明确说明哪些任务未完成。",
			}}
		},
	}
}

func GarbledCircuitBreaker() Hook {
	return Hook{
		Name: "garbled_circuit_breaker", Point: PostTurn,
		On: func(s State) *Result {
			if s.Get(StoreRespGarbled) >= 5 {
				stop := StopFormatError
				return &Result{Stop: &stop}
			}
			return nil
		},
	}
}
