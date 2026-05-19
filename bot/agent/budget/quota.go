package budget

import (
	"fmt"
	"strings"
)

// ToolQuota enforces per-turn limits on read and grep/glob calls,
// scaled dynamically by context watermark (green/yellow/red zones).
type ToolQuota struct {
	MaxReads    int
	MaxGreps    int
	Hard        bool // false=soft suggestion, true=enforced
	UsedReads   int
	UsedGreps   int
	extendCount int // max 2 Quota Request extensions per turn
}

// quotaSnapshot captures quota state for transaction rollback on interrupt.
type quotaSnapshot struct {
	UsedReads   int
	UsedGreps   int
	ExtendCount int
}

// ComputeQuota calculates this turn's quota from the context watermark.
// Uses percentage of budget, not absolute token count.
func ComputeQuota(usedTokens, tokenBudget int) ToolQuota {
	if tokenBudget <= 0 {
		return ToolQuota{MaxReads: 5, MaxGreps: 5, Hard: false}
	}
	// Use the SMALLER of percentage and absolute thresholds.
	// A 1M budget at 8% = 80K tokens — too generous. Cap at 64K green, 128K yellow.
	ratio := float64(usedTokens) / float64(tokenBudget)
	isGreen := ratio < 0.08 && usedTokens < 64000
	isYellow := (ratio < 0.15 && usedTokens < 128000) && !isGreen
	switch {
	case isGreen:
		return ToolQuota{MaxReads: 5, MaxGreps: 5, Hard: false}
	case isYellow:
		return ToolQuota{MaxReads: 3, MaxGreps: 2, Hard: true}
	default:
		return ToolQuota{MaxReads: 1, MaxGreps: 1, Hard: true}
	}
}

// ConsumeRead tries to consume one read from the quota.
// Returns an error message if the hard quota is exceeded.
func (q *ToolQuota) consumeRead() error {
	q.UsedReads++
	if !q.Hard {
		return nil
	}
	if q.UsedReads > q.MaxReads {
		return fmt.Errorf(quotaExhaustedMsg, "Read", q.MaxReads)
	}
	return nil
}

// ConsumeGrep tries to consume one grep/glob from the quota.
func (q *ToolQuota) consumeGrep() error {
	q.UsedGreps++
	if !q.Hard {
		return nil
	}
	if q.UsedGreps > q.MaxGreps {
		return fmt.Errorf(quotaExhaustedMsg, "Grep/Glob", q.MaxGreps)
	}
	return nil
}

const quotaExhaustedMsg = `[配额拦截] 本轮 %s 配额已耗尽（上限 %d）。请勿重新尝试。可选路径：
1. 利用已获取的信息进行逻辑推导
2. 使用 project_info 按需查询符号位置
3. 申请配额扩展（将扣除 30 衰减分）——如确需，系统会识别你的扩展请求
4. 直接进行实质性代码修改（edit）`

// TryExtend checks if the model's text output contains a quota extension request.
// Returns true if the request is recognized and quota should be reset.
// Max 2 extensions per turn.
func (q *ToolQuota) TryExtend(text string) bool {
	if q.extendCount >= 2 {
		return false
	}
	// Semantic detection — look for natural language intent, not literal format matching.
	// This prevents models from mechanically copy-pasting example formats.
	lower := strings.ToLower(text)
	indicators := []string{
		"申请额外配额", "申请配额扩展", "need more read", "need additional read",
		"需要更多读取", "需要额外配额", "quota extension", "request more reads",
	}
	for _, ind := range indicators {
		if strings.Contains(lower, ind) {
			q.extendCount++
			return true
		}
	}
	return false
}


// Snapshot captures current state for rollback.
func (q *ToolQuota) Snapshot() quotaSnapshot {
	return quotaSnapshot{
		UsedReads:   q.UsedReads,
		UsedGreps:   q.UsedGreps,
		ExtendCount: q.extendCount,
	}
}

// Rollback restores quota state from a snapshot.
func (q *ToolQuota) Rollback(snap quotaSnapshot) {
	q.UsedReads = snap.UsedReads
	q.UsedGreps = snap.UsedGreps
	q.extendCount = snap.ExtendCount
}

// ExtendCount returns how many quota extensions have been granted this turn.
func (q *ToolQuota) ExtendCount() int { return q.extendCount }

// ConsumeTool is a convenience method that routes by tool name.
// Returns an error message if the hard quota is exceeded.
func (q *ToolQuota) ConsumeTool(toolName string) error {
	switch toolName {
	case "read":
		return q.consumeRead()
	case "grep", "glob", "list":
		return q.consumeGrep()
	default:
		return nil // other tools are not quota-limited
	}
}
