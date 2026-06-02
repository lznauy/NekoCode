package hooks

import (
	"fmt"
	"strconv"
	"strings"
)

func VerificationHook() Hook {
	var injected bool
	return Hook{
		Name: "verification", Points: []HookPoint{PointPreTurn}, Priority: 10,
		On: func(s *Snapshot) *Result {
			if !s.Flag(KeyFileModified) { injected = false; return nil }
			if injected { return nil }
			injected = true
			return &Result{Hint: &Hint{Type: "verification", Severity: "warning",
				Content: "你修改了文件。在报告完成之前：先 build + test 确认修改正确，然后报告结果。如果已经验证过，忽略此提示。"}}
		},
	}
}

func QuotaHook() Hook {
	return Hook{
		Name: "quota", Points: []HookPoint{PointPreTurn}, Priority: 5,
		On: func(s *Snapshot) *Result {
			if s.Gauge(KeyQuotaHard) == 0 { return nil }
			left := s.Gauge(KeyQuotaReads)
			content := fmt.Sprintf("硬配额：剩余 %d 次读取、%d 次 grep。请使用已有信息。如需继续读取，在回复中申请配额扩展。",
				left, left)
			sev := "warning"
			if left <= 1 {
				sev = "critical"
				content += " 这是最后一次机会——立即总结并行动。"
			}
			return &Result{Hint: &Hint{Type: "quota", Severity: sev, Content: content}}
		},
	}
}

func UnfinishedWorkHook() Hook {
	return Hook{
		Name: "unfinished_work", Points: []HookPoint{PointPostTurn}, Priority: 10,
		On: func(s *Snapshot) *Result {
			if s.Flag(KeyFileModified) && s.Turn(KeyRespChat) > 0 && s.Gauge(KeyTasksAllDone) == 0 {
				return &Result{Hint: &Hint{Type: "verification", Severity: "critical",
					Content: "你还有未完成的任务。完成所有任务后再总结——不要在此停下，继续执行。"}}
			}
			return nil
		},
	}
}

func ExplorationExhaustedHook() Hook {
	var injected bool
	return Hook{
		Name: "exploration_exhausted", Points: []HookPoint{PointPreTurn}, Priority: 1,
		Suppresses: []string{"exploration_low"},
		On: func(s *Snapshot) *Result {
			if injected { return nil }
			if s.Gauge(KeyExploreScore) > 0 { return nil }
			injected = true
			return &Result{Hint: &Hint{Type: "exploration", Severity: "critical",
				Content: "你已经探索够了。不要再调用 read/grep/glob/list——继续搜索只会浪费轮次。基于已有信息，要么编辑/写入文件，要么报告发现。\n\n你的任务：" + s.Value(KeyStepInput)}}
		},
	}
}

func ExplorationLowHook() Hook {
	var injected bool
	return Hook{
		Name: "exploration_low", Points: []HookPoint{PointPreTurn}, Priority: 2,
		On: func(s *Snapshot) *Result {
			if injected || s.Flag(KeyFileModified) { return nil }
			score := s.Gauge(KeyExploreScore)
			if score >= 60 || score <= 0 { return nil }
			injected = true
			return &Result{Hint: &Hint{Type: "exploration", Severity: "warning",
				Content: "你还没有修改任何文件。用户要求你进行修改——停止分析，现在就开始编辑。"}}
		},
	}
}

func ExploreCascadeHook() Hook {
	var cascade int
	return Hook{
		Name: "explore_cascade", Points: []HookPoint{PointPostTool}, Priority: 1,
		Suppresses: []string{"exploration_low"},
		On: func(s *Snapshot) *Result {
			hasResearcher := s.Turn(KeyToolTaskResearcher) > 0
			if !hasResearcher { cascade = 0; return nil }
			if s.Flag(KeyFileModified) { cascade = 0; return nil }
			cascade++
			if cascade < 2 { return nil }
			return &Result{Hint: &Hint{Type: "explore_cascade", Severity: "warning",
				Content: "你已经启动了 " + strconv.Itoa(cascade) + " 个 researcher 子 Agent，但没有修改任何文件。如果需要文件内容，直接用 Read。如果已收集足够信息，立即综合发现并行动。\n\n你的任务：" + s.Value(KeyStepInput)}}
		},
	}
}

func GarbledToolCallHook() Hook {
	return Hook{
		Name: "garbled_tool_call", Points: []HookPoint{PointPostTurn}, Priority: 5,
		On: func(s *Snapshot) *Result {
			if s.Turn(KeyRespGarbled) > 0 {
				return &Result{Hint: &Hint{Type: "dsml_leak", Severity: "critical",
					Content: "上轮未通过 function calling 调用工具——请直接用 function calling 重试。"}}
			}
			return nil
		},
	}
}

func RepeatedToolCallHook() Hook {
	var lastSig string
	var repeatCount int
	return Hook{
		Name: "repeated_tool_call", Points: []HookPoint{PointPostTool}, Priority: 5,
		On: func(s *Snapshot) *Result {
			sig := s.Value(KeyToolSig)
			if sig == "" { return nil }
			if lastSig != sig { lastSig = sig; repeatCount = 1; return nil }
			repeatCount++
			if repeatCount < 2 { return nil }
			name := sig
			if idx := strings.IndexByte(name, '|'); idx >= 0 { name = name[:idx] }
			return &Result{Hint: &Hint{Type: "repeated_tool_call", Severity: "warning",
				Content: fmt.Sprintf("你已经用相同的参数调用了 %q %d 次——结果不会改变。停止重复同一个查询，基于已有信息继续前进。",
					name, repeatCount)}}
		},
	}
}

func GarbledCircuitBreaker() Hook {
	return Hook{
		Name: "garbled_circuit_breaker", Points: []HookPoint{PointPostTurn}, Priority: 1,
		On: func(s *Snapshot) *Result {
			if s.Counter(KeyRespGarbled) >= 3 {
				stop := StopFormatError
				return &Result{Stop: &stop}
			}
			return nil
		},
	}
}
