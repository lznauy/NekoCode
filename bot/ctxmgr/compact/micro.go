package compact

import (
	"sort"

)

// compactableTools are the tools whose results can be safely cleared.
var compactableTools = map[string]bool{
	"read": true, "bash": true, "grep": true, "glob": true,
	"web_search": true, "web_fetch": true, "edit": true, "write": true,
}

// Priority tiers for tool result retention during micro-compaction.
const (
	priorityLow    = iota // clear first: one-shot navigation
	priorityMedium        // clear second: valuable but time-sensitive
	priorityHigh          // clear last: file content referenced across turns
)

const ClearedMarker = "[Old tool result cleared]"

func compactableToolPriority(toolName, content string) int {
	switch toolName {
	case "read", "edit", "write":
		return priorityHigh
	case "bash":
		if len(content) > 120 {
			return priorityMedium
		}
		return priorityLow
	case "web_search", "web_fetch":
		return priorityMedium
	case "grep", "glob", "list":
		return priorityLow
	default:
		return priorityLow
	}
}

func (m *Compactor) lookupToolName(resultIdx int) string {
	msgs := m.Ctx.Messages
	targetID := msgs[resultIdx].ToolCallID
	if targetID == "" {
		return ""
	}
	for i := resultIdx - 1; i >= 0; i-- {
		if msgs[i].Role == "assistant" {
			for _, tc := range msgs[i].ToolCalls {
				if tc.ID == targetID {
					return tc.Function.Name
				}
			}
		}
	}
	return ""
}

type compactable struct {
	idx      int
	priority int
}

func (m *Compactor) lookupAssistantIdx(resultIdx int) int {
	msgs := m.Ctx.Messages
	targetID := msgs[resultIdx].ToolCallID
	if targetID == "" {
		return -1
	}
	for i := resultIdx - 1; i >= 0; i-- {
		if msgs[i].Role == "assistant" {
			for _, tc := range msgs[i].ToolCalls {
				if tc.ID == targetID {
					return i
				}
			}
		}
	}
	return -1
}

// microCompact clears old compactable tool results, keeping recent ones.
func (m *Compactor) microCompact() int {
	msgs := m.Ctx.Messages
	recentBoundary := m.findRecentTurnBoundary(2)
	if recentBoundary < 0 {
		recentBoundary = 0
	}

	type batch struct {
		assistantIdx int
		results      []compactable
	}
	batches := make(map[int]*batch)
	for i, msg := range msgs {
		if msg.Role != "tool" || msg.Content == ClearedMarker {
			continue
		}
		if !m.isCompactableResult(i) {
			continue
		}
		if i >= recentBoundary {
			continue
		}
		assistantIdx := m.lookupAssistantIdx(i)
		if assistantIdx < 0 {
			continue
		}
		toolName := m.lookupToolName(i)
		pri := compactableToolPriority(toolName, msg.Content)
		b := batches[assistantIdx]
		if b == nil {
			b = &batch{assistantIdx: assistantIdx}
			batches[assistantIdx] = b
		}
		b.results = append(b.results, compactable{idx: i, priority: pri})
	}

	var batchList []*batch
	for _, b := range batches {
		maxPri := priorityLow
		for _, r := range b.results {
			if r.priority > maxPri {
				maxPri = r.priority
			}
		}
		for i := range b.results {
			b.results[i].priority = maxPri
		}
		batchList = append(batchList, b)
	}

	sort.Slice(batchList, func(a, b int) bool {
		if batchList[a].results[0].priority != batchList[b].results[0].priority {
			return batchList[a].results[0].priority < batchList[b].results[0].priority
		}
		return batchList[a].assistantIdx < batchList[b].assistantIdx
	})

	keepResults := 3
	switch {
	case *m.TokenBudget >= 128000:
		keepResults = 8
	case *m.TokenBudget >= 64000:
		keepResults = 5
	}

	total := 0
	for _, b := range batchList {
		total += len(b.results)
	}
	if total <= keepResults {
		return 0
	}

	cleared := 0
	kept := total
	for _, b := range batchList {
		if kept-len(b.results) < keepResults {
			break
		}
		for _, r := range b.results {
			(m.Ctx.Messages)[r.idx].Content = ClearedMarker
			cleared++
		}
		kept -= len(b.results)
	}
	*m.CompactCount += cleared
	compactLog("micro_compact: cleared %d/%d tool results (%d kept, budget=%d)", cleared, total, keepResults, *m.TokenBudget)
	return cleared
}

func (m *Compactor) findRecentTurnBoundary(n int) int {
	msgs := m.Ctx.Messages
	userCount := 0
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "user" {
			userCount++
			if userCount >= n {
				return i
			}
		}
	}
	return 0
}

func (m *Compactor) isCompactableResult(resultIdx int) bool {
	msgs := m.Ctx.Messages
	targetID := msgs[resultIdx].ToolCallID
	if targetID == "" {
		return false
	}
	for i := resultIdx - 1; i >= 0; i-- {
		if msgs[i].Role == "assistant" {
			for _, tc := range msgs[i].ToolCalls {
				if tc.ID == targetID {
					return compactableTools[tc.Function.Name]
				}
			}
		}
	}
	return false
}

