// report.go — context usage report for /context command.
package ctxmgr

import (
	"fmt"
	"strings"

	"nekocode/bot/ctxmgr/compact"
	"nekocode/bot/ctxmgr/token"

	"charm.land/lipgloss/v2"
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
	CacheHitTokens  int
	CacheMissTokens int
	CacheHitRatio   float64
	SubCount        int
	SubTokens       int
	SubCacheHit     int
	SubCacheMiss    int
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
	r.CompactCount = m.CompactCount
	r.TrimCount = m.TrimCount
	r.Budget = m.ContextWindow
	r.CacheHitTokens, r.CacheMissTokens = m.Tracker.CacheStats()
	r.CacheHitRatio = m.Tracker.CacheHitRatio()
	sub := m.Tracker.SubStats()
	r.SubCount = sub.Count
	r.SubTokens = sub.TotalTokens
	r.SubCacheHit = sub.CacheHitTokens
	r.SubCacheMiss = sub.CacheMissTokens
	return r
}

func estimateSkills(skillList string) []SkillToken {
	if skillList == "" {
		return nil
	}
	var skills []SkillToken
	for line := range strings.SplitSeq(skillList, "\n") {
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
func FormatContextReport(r ContextReport) string {
	used := r.SystemPrompt + r.ToolDefTokens + r.TodoText + r.SkillList + r.Messages
	free := r.Budget - used
	if free < 0 {
		free = 0
	}
	pct := func(n int) string {
		if r.Budget == 0 {
			return ""
		}
		return fmt.Sprintf("(%.0f%%)", float64(n)/float64(r.Budget)*100)
	}
	item := func(ch, label string, n int) string {
		return barColors[ch].Render(barChars[ch]) + " " + label + ": " + FormatTokens(n) + " " + barColors["free"].Render(pct(n))
	}

	bar := BuildBar(r.Budget, []BarSegment{
		{Size: r.SystemPrompt, Kind: "sys"},
		{Size: r.ToolDefTokens + r.TodoText, Kind: "tools"},
		{Size: r.SkillList, Kind: "skills"},
		{Size: r.Messages, Kind: "msgs"},
		{Size: free, Kind: "free"},
	}, 20)

	s := fmt.Sprintf("%s  %s / %s\n\n%s  %s\n%s  %s\n\n%s",
		bar, FormatTokens(used), FormatTokens(r.Budget),
		item("sys", "System", r.SystemPrompt),
		item("tools", "Tools", r.ToolDefTokens),
		item("msgs", "Messages", r.Messages),
		item("skills", "Skills", r.SkillList),
		barColors["free"].Render(fmt.Sprintf("%d tools · %d msgs · %d archived  %s Free: %s",
			r.ToolDefCount, r.UserMessages+r.AssistantMsgs+r.ToolResults, r.Archived,
			FormatTokens(free), pct(free))),
	)

	if r.CacheHitTokens > 0 || r.CacheMissTokens > 0 {
		hit := FormatTokens(r.CacheHitTokens)
		miss := FormatTokens(r.CacheMissTokens)
		ratio := fmt.Sprintf("%.0f%%", r.CacheHitRatio*100)
		s += fmt.Sprintf("\n%s Cache: hit %s / miss %s · %s",
			barColors["cache"].Render(barChars["cache"]), hit, miss, ratio)
	}

	if r.SubCount > 0 {
		subTok := FormatTokens(r.SubTokens)
		subHit := FormatTokens(r.SubCacheHit)
		subMiss := FormatTokens(r.SubCacheMiss)
		var subRatio string
		if total := r.SubCacheHit + r.SubCacheMiss; total > 0 {
			subRatio = fmt.Sprintf(" · hit %.0f%%", float64(r.SubCacheHit)/float64(total)*100)
		}
		s += fmt.Sprintf("\n%s Subagents: %d runs · %s tokens · hit %s / miss %s%s",
			barColors["sub"].Render(barChars["sub"]), r.SubCount, subTok, subHit, subMiss, subRatio)
	}
	return s
}

func FormatTokens(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fm", float64(n)/1_000_000)
	case n >= 1000:
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

type BarSegment struct {
	Size int
	Kind string
}

var barColors= map[string]lipgloss.Style{
	"sys":    lipgloss.NewStyle().Foreground(lipgloss.Color("#888")),
	"tools":  lipgloss.NewStyle().Foreground(lipgloss.Color("#999")),
	"todo":   lipgloss.NewStyle().Foreground(lipgloss.Color("#d47757")),
	"skills": lipgloss.NewStyle().Foreground(lipgloss.Color("#ffc107")),
	"msgs":   lipgloss.NewStyle().Foreground(lipgloss.Color("#9334ea")),
	"free":   lipgloss.NewStyle().Foreground(lipgloss.Color("#666")),
	"cache":  lipgloss.NewStyle().Foreground(lipgloss.Color("#6ab")),
	"sub":    lipgloss.NewStyle().Foreground(lipgloss.Color("#6ab")),
}

var barChars = map[string]string{
	"sys": "⛁", "tools": "⛁", "todo": "⛀", "skills": "⛀", "msgs": "⛁", "free": "⛶",
	"cache": "⛂", "sub": "⛃",
}

func BuildBar(total int, segments []BarSegment, width int) string {
	if total <= 0 {
		return ""
	}
	allocated := make([]int, len(segments))
	remaining := width
	for i, s := range segments {
		if s.Size > 0 {
			w := s.Size * width / total
			if w < 1 {
				w = 1
			}
			allocated[i] = w
			remaining -= w
		}
	}
	for i := len(segments) - 1; i >= 0 && remaining > 0; i-- {
		if segments[i].Size > 0 {
			allocated[i] += remaining
			break
		}
	}

	var b strings.Builder
	for i, s := range segments {
		if allocated[i] <= 0 {
			continue
		}
		ch := barChars[s.Kind]
		if ch == "" {
			ch = " "
		}
		sty := barColors[s.Kind]
		for range allocated[i] {
			fmt.Fprintf(&b, "%s ", sty.Render(ch))
		}
	}
	return b.String()
}
