// header.go — compact operational status for cache, context and worktree health.
package components

import (
	"fmt"
	"strconv"
	"strings"

	"nekocode/interaction/tui/styles"
	"nekocode/protocol"
	"nekocode/util/text"

	"charm.land/lipgloss/v2"
)

var (
	headerAddedStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color(styles.DiffGreen))
	headerDeletedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(styles.Red))
)

type Header struct {
	Width   int
	Version string

	cacheHit      int
	cacheMiss     int
	contextTokens int
	contextBudget int
	compactionAt  int
	workspace     protocol.WorkspaceChanges
}

type headerStatusProfile struct {
	averageLabel string
	compact      bool
	tight        bool
	workspace    int
}

const (
	workspaceHidden = iota
	workspaceCompact
	workspaceVerbose
)

func NewHeader(width int, version string) *Header {
	return &Header{Width: width, Version: version}
}

func (h *Header) SetWidth(width int) { h.Width = width }
func (h *Header) Height() int        { return 2 }

func (h *Header) SetContext(used, budget, compactionAt, cacheHit, cacheMiss int) {
	h.contextTokens = used
	h.contextBudget = budget
	h.compactionAt = compactionAt
	h.cacheHit = cacheHit
	h.cacheMiss = cacheMiss
}

func (h *Header) SetWorkspace(changes protocol.WorkspaceChanges) { h.workspace = changes }

func (h *Header) View() string {
	w := max(20, h.Width)
	content := h.fitContent(w)
	line := styles.BorderStyle.Render(strings.Repeat(styles.Horizontal, w))
	return content + strings.Repeat(" ", max(0, w-lipgloss.Width(content))) + "\n" + line + "\n"
}

func (h *Header) fitContent(width int) string {
	profiles := []headerStatusProfile{
		{averageLabel: "平均命中 ", workspace: workspaceVerbose},
		{averageLabel: "平均 ", compact: true, workspace: workspaceCompact},
		{averageLabel: "均", compact: true, tight: true, workspace: workspaceCompact},
		{averageLabel: "平均 ", compact: true},
		{averageLabel: "均", compact: true, tight: true},
		{compact: true},
	}
	if !h.workspace.Available {
		profiles = profiles[3:]
	}
	for _, showVersion := range []bool{true, false} {
		brand := h.brand(showVersion)
		for _, profile := range profiles {
			content := brand + separator() + h.status(profile)
			if lipgloss.Width(content) <= width {
				return content
			}
		}
	}
	return h.brand(false)
}

func (h *Header) brand(showVersion bool) string {
	cat := styles.CatBodyStyle.Render("(=") + styles.CatEyeStyle.Render("^.^") + styles.CatBodyStyle.Render("=)")
	brand := cat + " " + styles.PrimaryStyle.Bold(true).Render("NEKOCODE")
	if showVersion {
		// Version already carries its tag prefix ("v0.4.8", or "dev" for
		// local builds); prepending another "v" printed "vv0.4.8".
		brand += " " + styles.SubtleStyle.Render(h.Version)
	}
	return brand
}

func (h *Header) status(profile headerStatusProfile) string {
	parts := make([]string, 0, 4)
	if profile.averageLabel != "" {
		parts = append(parts, profile.averageLabel+hitRatio(h.cacheHit, h.cacheMiss, h.cacheHit+h.cacheMiss > 0))
	}
	contextLabel := h.contextLabel(profile.compact)
	compactionLabel := h.compactionLabel(profile.compact)
	if profile.tight {
		contextLabel = strings.Replace(contextLabel, " ", "", 1)
		compactionLabel = strings.Replace(compactionLabel, " ", "", 1)
	}
	parts = append(parts, contextLabel, compactionLabel)
	if h.workspace.Available && profile.workspace != workspaceHidden {
		parts = append(parts, h.workspaceLabel(profile.workspace == workspaceVerbose))
	}
	return styles.MutedStyle.Render(strings.Join(parts, " · "))
}

func (h *Header) contextLabel(compact bool) string {
	percent := percentOf(h.contextTokens, h.contextBudget)
	if compact {
		return fmt.Sprintf("上下文 %s/%d%%", text.FormatTokens(h.contextTokens), percent)
	}
	return fmt.Sprintf("上下文 %s (%d%%)", text.FormatTokens(h.contextTokens), percent)
}

func (h *Header) compactionLabel(compact bool) string {
	remaining := max(0, h.compactionAt-h.contextTokens)
	if compact {
		return fmt.Sprintf("距压 %d%%", percentOf(remaining, h.contextBudget))
	}
	return fmt.Sprintf("距压缩 %d%%", percentOf(remaining, h.contextBudget))
}

func (h *Header) workspaceLabel(verbose bool) string {
	added := headerAddedStyle.Render("+" + formatCount(h.workspace.Added))
	deleted := headerDeletedStyle.Render("−" + formatCount(h.workspace.Deleted))
	if verbose {
		untracked := styles.YellowStyle.Render(formatCount(h.workspace.Untracked))
		return styles.SubtleStyle.Render("工作区 ") + added + " " + deleted +
			styles.BorderStyle.Render(" · ") + styles.SubtleStyle.Render("未跟踪 ") + untracked
	}
	untracked := styles.YellowStyle.Render("?" + formatCount(h.workspace.Untracked))
	return styles.SubtleStyle.Render("git ") + strings.Join([]string{added, deleted, untracked}, " ")
}

func formatCount(value int) string {
	digits := strconv.Itoa(max(value, 0))
	for i := len(digits) - 3; i > 0; i -= 3 {
		digits = digits[:i] + "," + digits[i:]
	}
	return digits
}

func hitRatio(hit, miss int, reported bool) string {
	if !reported || hit+miss <= 0 {
		return "—"
	}
	return fmt.Sprintf("%.2f%%", float64(hit)*100/float64(hit+miss))
}

func percentOf(value, total int) int {
	if value <= 0 || total <= 0 {
		return 0
	}
	return min(100, int(float64(value)*100/float64(total)+0.5))
}

func separator() string { return styles.BorderStyle.Render(" · ") }
