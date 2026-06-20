package builtin

import "fmt"

func QuotaHook() Hook {
	return Hook{
		Name: "quota", Point: PreTurn,
		On: func(s State) *Result {
			left := s.Get(StoreQuotaReads)
			if left > 2 {
				s.Set(CounterQuotaWarned, 0)
				return nil
			}
			if left == s.Get(CounterQuotaWarned) {
				return nil
			}
			s.Set(CounterQuotaWarned, left)
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
