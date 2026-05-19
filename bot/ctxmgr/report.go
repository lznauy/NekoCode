// report.go — context usage report for /context command.
package ctxmgr

import (
	"fmt"
	"strings"

	"nekocode/bot/ctxmgr/compact"
	"nekocode/bot/ctxmgr/token"
)

type ContextReport struct {
	Budget         int
	SystemPrompt   int
	TodoText       int
	SkillList      int
	ToolDefTokens  int
	SkillTokens    []SkillToken
	Messages       int
	Archived       int
	ClearedMarkers int
	CompactCount   int
	TrimCount      int
	ToolDefCount   int
	UserMessages   int
	SysInjections  int
	AssistantMsgs  int
	ToolResults    int
	CacheHitTokens int
	CacheMissTokens int
	CacheHitRatio  float64
}

type SkillToken struct {
	Name   string
	Tokens int
}

func (m *Manager) Report() ContextReport {
	m.mu.RLock()
	defer m.mu.RUnlock()

	r := ContextReport{}
	r.SystemPrompt = token.EstimateString(m.ctx.SystemPrompt)
	r.TodoText = token.EstimateString(m.ctx.Todo)
	r.SkillList = token.EstimateString(m.ctx.Skills)
	r.SkillTokens = estimateSkills(m.ctx.Skills)

	for i := m.ctx.CompactBoundary; i < len(m.ctx.Messages); i++ {
		msg := m.ctx.Messages[i]
		if msg.Content == compact.ClearedMarker {
			r.ClearedMarkers++
			continue
		}
		switch msg.Role {
		case "user":
			if strings.HasPrefix(msg.Content, "[System]") {
				r.SysInjections++
			} else {
				r.UserMessages++
			}
		case "assistant":
			r.AssistantMsgs++
		case "tool":
			r.ToolResults++
		}
	}
	r.Messages = token.EstimateTokens(m.ctx.Messages[m.ctx.CompactBoundary:])
	r.Archived = m.ctx.CompactBoundary
	r.CompactCount = m.tok.CompactCount
	r.TrimCount = m.tok.TrimCount
	r.Budget = m.tok.TokenBudget
	r.CacheHitTokens, r.CacheMissTokens = m.tok.Tracker.CacheStats()
	r.CacheHitRatio = m.tok.Tracker.CacheHitRatio()
	return r
}

func estimateSkills(skillList string) []SkillToken {
	if skillList == "" {
		return nil
	}
	var skills []SkillToken
	for _, line := range strings.Split(skillList, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "- ") {
			continue
		}
		content := strings.TrimPrefix(line, "- ")
		content = strings.TrimPrefix(content, "**")
		if idx := strings.Index(content, "**:"); idx > 0 {
			content = content[:idx] + content[idx+2:]
		}
		if idx := strings.Index(content, ":"); idx > 0 {
			name := content[:idx]
			tokens := token.EstimateString(line)
			skills = append(skills, SkillToken{Name: strings.TrimSpace(name), Tokens: tokens})
		}
	}
	return skills
}

// FormatContextReport renders a context report as a styled string.
const nbsp = " "

func indent(n int) string { return strings.Repeat(nbsp, n) }

func FormatContextReport(r ContextReport) string {
	used := r.SystemPrompt  + r.ToolDefTokens + r.TodoText + r.SkillList + r.Messages
	free := r.Budget - used
	if free < 0 {
		free = 0
	}
	pct := func(n int) string {
		if r.Budget == 0 {
			return "—"
		}
		return fmt.Sprintf("%.1f%%", float64(n)/float64(r.Budget)*100)
	}

	var b strings.Builder

	b.WriteString("Context Report\n")
	bar := buildBar(r.Budget, []barSegment{
		{size: r.SystemPrompt, label: "", kind: "sys"},
		{size: r.ToolDefTokens, label: "", kind: "tools"},
		{size: r.TodoText, label: "", kind: "todo"},
		{size: r.SkillList, label: "", kind: "skills"},
		{size: r.Messages, label: "", kind: "msgs"},
		{size: free, label: "", kind: "free"},
	}, 40)
	b.WriteString(bar + "\n")
	fmt.Fprintf(&b, "%s / %s (%s)\n\n", formatTokens(used), formatTokens(r.Budget), pct(used))

	b.WriteString(indent(0) + "▸ System\n")
	fmt.Fprintf(&b, indent(2)+"%-20s %s (%s)\n", "Prompt", formatTokens(r.SystemPrompt), pct(r.SystemPrompt))
	fmt.Fprintf(&b, indent(2)+"%-20s %s (%s) · %d tools\n", "Tool definitions", formatTokens(r.ToolDefTokens), pct(r.ToolDefTokens), r.ToolDefCount)
	if r.CacheHitTokens > 0 || r.CacheMissTokens > 0 {
		b.WriteString("\n" + indent(0) + "▸ Cache\n")
		fmt.Fprintf(&b, indent(2)+"%-20s %s\n", "Hit tokens", formatTokens(r.CacheHitTokens))
		fmt.Fprintf(&b, indent(2)+"%-20s %s\n", "Miss tokens", formatTokens(r.CacheMissTokens))
		fmt.Fprintf(&b, indent(2)+"%-20s %.1f%%\n", "Hit ratio", r.CacheHitRatio*100)
	}

	total := r.UserMessages + r.AssistantMsgs + r.ToolResults + r.SysInjections
	if total > 0 || r.Archived > 0 || r.ClearedMarkers > 0 {
		b.WriteString("\n" + indent(0) + "▸ Messages\n")
		fmt.Fprintf(&b, indent(2)+"%-20s %s (%s)\n", "Total", formatTokens(r.Messages), pct(r.Messages))
		fmt.Fprintf(&b, indent(2)+"%-20s %d\n", "User messages", r.UserMessages)
		fmt.Fprintf(&b, indent(2)+"%-20s %d\n", "Assistant", r.AssistantMsgs)
		fmt.Fprintf(&b, indent(2)+"%-20s %d\n", "Tool results", r.ToolResults)
		if r.SysInjections > 0 {
			fmt.Fprintf(&b, indent(2)+"%-20s %d\n", "[System] hints", r.SysInjections)
		}
		if r.Archived > 0 {
			fmt.Fprintf(&b, indent(2)+"%-20s %d messages\n", "Archived", r.Archived)
			if r.TrimCount > 0 {
				fmt.Fprintf(&b, indent(2)+"%-20s %d messages\n", "Trimmed", r.TrimCount)
			}
		}
		if r.ClearedMarkers > 0 {
			fmt.Fprintf(&b, indent(2)+"%-20s %d (total %d)\n", "Compacted", r.ClearedMarkers, r.CompactCount)
		}
	}

	return b.String()
}

type barSegment struct {
	size  int
	label string
	kind  string
}

func buildBar(total int, segments []barSegment, width int) string {
	if total <= 0 {
		return ""
	}
	allocated := make([]int, len(segments))
	remaining := width
	for i, s := range segments {
		if s.size > 0 {
			w := s.size * width / total
			if w < 1 {
				w = 1
			}
			allocated[i] = w
			remaining -= w
		}
	}
	for i := len(segments) - 1; i >= 0 && remaining > 0; i-- {
		if segments[i].size > 0 {
			allocated[i] += remaining
			break
		}
	}

	chars := map[string]string{
		"sys": "▨", "tools": "▩", "anchor": "◈", "todo": "○",
		"skills": "◆", "msgs": "▣", "free": "·",
	}

	var b strings.Builder
	for i, s := range segments {
		if allocated[i] <= 0 {
			continue
		}
		ch := chars[s.kind]
		if ch == "" {
			ch = " "
		}
		b.WriteString(strings.Repeat(ch, allocated[i]))
	}
	return b.String()
}

func formatTokens(n int) string {
	if n >= 1000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprintf("%d", n)
}
