package budget

import (
	"fmt"

	"nekocode/bot/policy/semantics"
)

const quotaExhaustedMsg = `[配额] 本轮读取配额已达上限 (%d)。基于已有信息继续，不要重试。`

// ToolQuota enforces per-turn limits on exploratory/source-producing tool
// calls, scaled dynamically by context usage percentage.
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
		return ToolQuota{MaxSlots: 8}
	case ratio < 0.30:
		return ToolQuota{MaxSlots: 4}
	default:
		return ToolQuota{MaxSlots: 2}
	}
}

func (q *ToolQuota) consume() error {
	q.Used++
	if q.Used > q.MaxSlots {
		return fmt.Errorf(quotaExhaustedMsg, q.MaxSlots)
	}
	return nil
}

// ConsumeTool routes by tool name. Returns error if quota exhausted.
// Only consumes quota for information-gathering tools.
func (q *ToolQuota) ConsumeTool(toolName string) error {
	if semantics.ClassifyToolCall(toolName, nil).Exploratory {
		return q.consume()
	}
	return nil
}

func (q *ToolQuota) ConsumeCall(toolName string, args map[string]any) error {
	sem := semantics.ClassifyToolCall(toolName, args)
	if sem.Exploratory {
		return q.consume()
	}
	return nil
}
