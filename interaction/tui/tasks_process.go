package tui

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"nekocode/interaction/tui/components"
	"nekocode/interaction/tui/styles"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

const taskProcessCacheLimit = 2048

type taskProcessPage struct {
	list        *components.List
	calls       []taskProcessCall
	selected    string
	follow      bool
	unread      int
	initialized bool
	seekOldest  bool
}

type taskProcessCall struct {
	entry    taskEntry
	sequence int
	done     bool
	duration string
}

type taskProcessRow struct {
	number                           int
	id, name, args, status, duration string
	selected                         bool
}

func (r taskProcessRow) Height(int) int { return 2 }
func (r taskProcessRow) Render(width int) string {
	sty := styles.DefaultStyles()
	marker := "  "
	if r.selected {
		marker = "▸ "
	}
	heading := fmt.Sprintf("%s%03d  %s · %s", marker, r.number, strings.Join(strings.Fields(ansi.Strip(r.name)), " "), r.status)
	if r.duration != "" {
		heading += " · " + r.duration
	}
	heading = ansi.Truncate(heading, max(1, width), "…")
	detail := ansi.Truncate("       "+r.args, max(1, width), "…")
	if r.selected {
		style := sty.Primary.Bold(true).Background(lipgloss.Color(styles.BtnYesBg))
		return style.Render(heading+strings.Repeat(" ", max(0, width-lipgloss.Width(heading)))) + "\n" + style.Render(detail+strings.Repeat(" ", max(0, width-lipgloss.Width(detail))))
	}
	return sty.Base.Render(heading) + "\n" + sty.Muted.Render(detail)
}

func processArgumentSummary(arguments string) string {
	var fields map[string]any
	if json.Unmarshal([]byte(arguments), &fields) == nil {
		for _, key := range []string{"command", "path", "query", "pattern", "prompt", "action"} {
			if value, ok := fields[key].(string); ok && value != "" {
				return strings.Join(strings.Fields(ansi.Strip(value)), " ")
			}
		}
	}
	return strings.Join(strings.Fields(ansi.Strip(arguments)), " ")
}

func (m *Model) enterTaskProcess() {
	d := m.tasks.detail
	if d.process == nil {
		d.process = &taskProcessPage{list: components.NewList(), follow: true}
	}
	d.showProcess = true
	m.rebuildTaskProcess()
}
func (m *Model) leaveTaskProcess() {
	d := m.tasks.detail
	anchor, line := d.list.ViewportAnchor()
	d.process = nil
	d.showProcess = false
	m.rebuildTaskDetail()
	d.list.RestoreViewportAnchor(anchor, line)
}

func (m *Model) rebuildTaskProcess() {
	d := m.tasks.detail
	if d == nil || d.process == nil {
		return
	}
	p := d.process
	positions := make(map[string]int, len(p.calls))
	latestSequence := 0
	for i, c := range p.calls {
		positions[c.entry.call.ID] = i
		latestSequence = max(latestSequence, c.sequence)
	}
	records := d.visibleRecords()
	sequences := map[string]int{}
	for _, record := range records {
		for _, call := range record.ToolCalls {
			sequences[call.ID] = record.Sequence
		}
	}
	for _, call := range d.processingToolCalls(records) {
		if call.Name == "nekocode_submit_result" {
			continue
		}
		call, result := taskToolSnapshot(d, call)
		index, known := positions[call.ID]
		if !known {
			index = len(p.calls)
			positions[call.ID] = index
			p.calls = append(p.calls, taskProcessCall{})
			if p.initialized && !p.follow && !p.seekOldest && (sequences[call.ID] == 0 || latestSequence == 0 || sequences[call.ID] > latestSequence) {
				p.unread++
			}
		}
		cached := &p.calls[index]
		copyCall := call
		cached.entry.key = "process:" + call.ID
		cached.entry.call = &copyCall
		if sequence := sequences[call.ID]; sequence > 0 {
			cached.sequence = sequence
		}
		if result.ToolCallID != "" || result.Content != "" || result.OutputPath != "" || result.Sequence > 0 {
			cached.entry.result = result
		}
		if result.ToolCallID != "" || result.Sequence > 0 {
			cached.done = true
		}
		if live, ok := d.tools[call.ID]; ok {
			cached.done = cached.done || live.done
			var first, last int64
			for _, execution := range live.payload.Evidence.Executions {
				if execution.StartedAt > 0 && (first == 0 || execution.StartedAt < first) {
					first = execution.StartedAt
				}
				last = max(last, execution.EndedAt)
			}
			if first > 0 && last >= first {
				cached.duration = time.Duration(last - first).Round(time.Millisecond).String()
			}
		}
	}
	sort.SliceStable(p.calls, func(i, j int) bool {
		a, b := p.calls[i].sequence, p.calls[j].sequence
		if a == 0 || b == 0 {
			return a > 0 && b == 0
		}
		return a < b
	})
	// Keep the selected call pinned if a long-lived task exceeds the cache.
	for len(p.calls) > taskProcessCacheLimit {
		index := 0
		if p.seekOldest {
			index = len(p.calls) - 1
		}
		if p.calls[index].entry.call.ID == p.selected {
			if index == 0 {
				index = 1
			} else {
				index--
			}
		}
		p.calls = append(p.calls[:index], p.calls[index+1:]...)
	}
	p.initialized = true
	if len(p.calls) > 0 {
		if p.follow {
			p.selected = p.calls[len(p.calls)-1].entry.call.ID
			p.unread = 0
		}
		if p.selected == "" {
			p.selected = p.calls[0].entry.call.ID
		}
	}
	m.renderTaskProcessRows()
}

func (m *Model) renderTaskProcessRows() {
	d := m.tasks.detail
	p := d.process
	if p == nil {
		return
	}
	anchor, line := p.list.ViewportAnchor()
	anchorID := ""
	if anchor < len(p.list.Items()) {
		if old, ok := p.list.Items()[anchor].(taskProcessRow); ok {
			anchorID = old.id
		}
	}
	items := make([]components.Item, 0, len(p.calls))
	for i, c := range p.calls {
		status := "执行中"
		if c.done {
			status = "完成"
			if c.entry.result.IsError {
				status = "失败"
			}
		} else if !d.active() {
			status = "未返回"
		}
		items = append(items, taskProcessRow{id: c.entry.call.ID, number: i + 1, name: c.entry.call.Name, args: processArgumentSummary(c.entry.call.Arguments), status: status, duration: c.duration, selected: c.entry.call.ID == p.selected})
	}
	p.list.SetItems(items...)
	p.list.SetGap(1)
	w, h := m.taskSize()
	width, height := taskContentSize(w, h)
	p.list.SetSize(width, height)
	if p.follow {
		p.list.ScrollToBottom()
	} else {
		for i, c := range p.calls {
			if c.entry.call.ID == anchorID {
				anchor = i
				break
			}
		}
		p.list.RestoreViewportAnchor(anchor, line)
	}
}

func (m *Model) handleTaskProcessKey(key string) tea.Cmd {
	d := m.tasks.detail
	p := d.process
	if key == "esc" || key == "p" {
		m.leaveTaskProcess()
		return nil
	}
	if key == "r" {
		return tea.Batch(m.loadTaskHistory(d.currentCursor()), m.loadTaskAttempts())
	}
	if key == "[" {
		return m.stepTaskAttempt(-1)
	}
	if key == "]" {
		return m.stepTaskAttempt(1)
	}
	if key == "end" {
		p.follow = true
		p.seekOldest = false
		p.unread = 0
		m.rebuildTaskProcess()
		return tea.Batch(m.loadTaskHistory(taskAttemptCursor(d.attempt)), m.scheduleTaskFrame())
	}
	if len(p.calls) == 0 {
		return nil
	}
	index := 0
	for i, c := range p.calls {
		if c.entry.call.ID == p.selected {
			index = i
			break
		}
	}
	switch key {
	case "enter":
		return m.inspectTaskTool(p.calls[index].entry)
	case "up", "down", "pgup", "pgdown", "home":
		p.follow = false
		delta := 1
		if key == "pgup" || key == "pgdown" {
			delta = max(1, p.list.Height()/3)
		}
		if key == "up" || key == "pgup" {
			delta = -delta
		}
		target := max(0, min(len(p.calls)-1, index+delta))
		if key == "home" {
			target = 0
			p.seekOldest = d.historyHasMore()
		}
		p.selected = p.calls[target].entry.call.ID
		m.renderTaskProcessRows()
		top := p.list.ScrollY()
		start := target * 3
		end := start + 2
		if start < top {
			p.list.RestoreViewportAnchor(target, 0)
		} else if end > top+p.list.Height() {
			p.list.ScrollBy(end - top - p.list.Height())
		}
		if target == 0 && (index == 0 || key == "home") && d.historyHasMore() && !d.loading {
			return m.loadTaskHistory(d.page.Before)
		}
	}
	return nil
}
