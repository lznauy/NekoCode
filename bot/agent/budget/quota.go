package budget

import "fmt"

// ToolQuota enforces per-turn limits on information-gathering tools (read/grep/glob/list),
// scaled dynamically by context usage percentage.
type ToolQuota struct {
	MaxSlots int
	Used     int
}

// ComputeQuota calculates this turn's quota from the context watermark.
func ComputeQuota(usedTokens, contextWindow int) ToolQuota {
	if contextWindow <= 0 {
		return ToolQuota{MaxSlots: 3}
	}
	ratio := float64(usedTokens) / float64(contextWindow)
	switch {
	case ratio < 0.15:
		return ToolQuota{MaxSlots: 4}
	case ratio < 0.30:
		return ToolQuota{MaxSlots: 2}
	default:
		return ToolQuota{MaxSlots: 1}
	}
}

func (q *ToolQuota) consume() error {
	q.Used++
	if q.Used > q.MaxSlots {
		return fmt.Errorf(quotaExhaustedMsg, q.MaxSlots)
	}
	return nil
}

const quotaExhaustedMsg = `[配额] 本轮读取配额已达上限 (%d)。基于已有信息继续，不要重试。`

// ConsumeTool routes by tool name. Returns error if quota exhausted.
func (q *ToolQuota) ConsumeTool(toolName string) error {
	switch toolName {
	case "read", "grep", "glob", "list":
		return q.consume()
	}
	return nil
}
