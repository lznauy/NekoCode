package tui

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"nekocode/interaction/tui/components"
	"nekocode/interaction/tui/components/block"
	"nekocode/interaction/tui/components/message"
	"nekocode/interaction/tui/components/processing"
	"nekocode/interaction/tui/styles"
	controlruntime "nekocode/runtime"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

type taskEntry struct {
	key         string
	item        components.Item
	call        *controlruntime.SubagentToolCall
	result      controlruntime.SubagentRecord
	renderState any
}

func (m *Model) taskSize() (int, int) {
	w, h := m.taskTerminalSize()
	if w >= 60 {
		w -= 6
	}
	if h >= 16 {
		h -= 4
	}
	return max(1, w), max(1, h)
}

func (m *Model) taskTerminalSize() (int, int) {
	w, h := m.Width, m.Height
	if m.terminalWidth > 0 {
		w = m.terminalWidth
	}
	if m.terminalHeight > 0 {
		h = m.terminalHeight
	}
	return max(1, w), max(1, h)
}

func (m *Model) resizeTasks() {
	if m.tasks == nil || m.tasks.detail == nil {
		return
	}
	w, h := m.taskSize()
	d := m.tasks.detail
	if d.list != nil {
		inner, bodyHeight := taskContentSize(w, h)
		anchor, line := d.list.ViewportAnchor()
		d.list.SetSize(inner, bodyHeight)
		if !d.follow {
			d.list.RestoreViewportAnchor(anchor, line)
		}
		if d.follow {
			d.list.ScrollToBottom()
		}
	}
	if d.inspector != nil {
		inner, bodyHeight := taskContentSize(w, h)
		anchor, line := d.inspector.ViewportAnchor()
		d.inspector.SetSize(inner, bodyHeight)
		d.inspector.RestoreViewportAnchor(anchor, line)
	}
}

// The host view remains intact under this layer. Composing to a bounded cell
// canvas also clips wide tool/code lines safely at very small terminal sizes.
func (m *Model) taskOverlayView(background string) string {
	w, h := m.taskSize()
	panel := m.taskPanelView(w, h)
	terminalWidth, terminalHeight := m.taskTerminalSize()
	canvas := lipgloss.NewCanvas(terminalWidth, terminalHeight)
	canvas.Compose(lipgloss.NewLayer(background))
	x, y := (terminalWidth-w)/2, (terminalHeight-h)/2
	canvas.Compose(lipgloss.NewCompositor(lipgloss.NewLayer(panel).X(x).Y(y)))
	return canvas.Render()
}

func (m *Model) taskAttemptsView(width int) string {
	d := m.tasks.detail
	attempts := d.attemptDisplayList()
	if len(attempts) == 0 {
		if d.attemptsLoading {
			return "正在加载轮次…"
		}
		if d.attemptsErr != "" {
			return fmt.Sprintf("轮次加载失败：%s · r 重试", d.attemptsErr)
		}
		return "旧版记录，没有独立执行轮次。"
	}
	lines := make([]string, 0, len(attempts)+2)
	for i, attempt := range attempts {
		prefix, current := "  ", " "
		if i == d.attemptIndex {
			prefix = "› "
		}
		if d.attempt != nil && d.attempt.ID == attempt.ID {
			current = "●"
		}
		summary := strings.Join(strings.Fields(ansi.Strip(attempt.Result)), " ")
		if summary == "" {
			summary = strings.Join(strings.Fields(ansi.Strip(attempt.Prompt)), " ")
		}
		line := fmt.Sprintf("%s%s 第 %d 轮 · %-12s · %s", prefix, current, attempt.Seq, taskStatus(attempt.Status), summary)
		line = ansi.Truncate(ansi.Strip(line), width, "…")
		if i == d.attemptIndex {
			line = lipgloss.NewStyle().Foreground(lipgloss.Color("#e0e0e0")).Background(lipgloss.Color("#303a3a")).Width(width).Render(line)
		}
		lines = append(lines, line)
	}
	if d.attemptsLoading {
		lines = append(lines, "正在加载轮次…")
	} else if d.attemptsErr != "" {
		lines = append(lines, fmt.Sprintf("轮次加载失败：%s · r 重试", d.attemptsErr))
	} else if d.attempts.HasMore {
		lines = append(lines, "↓ 加载更早轮次")
	}
	return strings.Join(lines, "\n")
}

func (m *Model) taskPanelView(w, h int) string {
	s := m.tasks
	if w < 20 || h < 6 {
		lines := make([]string, h)
		lines[0] = "子任务"
		if h > 2 {
			lines[1] = "请放大终端查看"
		}
		lines[h-1] = "Esc 返回"
		return fillTaskBackground(fitTaskBody(strings.Join(lines, "\n"), w, h), w, h)
	}
	inner, bodyHeight := taskContentSize(w, h)
	sty := styles.DefaultStyles()
	title := "子任务"
	status, footer, body := "", "↑↓ 选择 · Enter 查看 · / 搜索 · Esc 关闭", ""
	if d := s.detail; d != nil {
		title = "‹ 子任务 / " + taskLabel(d.info)
		if d.attempt != nil {
			title += fmt.Sprintf(" · 第 %d 轮", d.attempt.Seq)
		}
		status = taskStatus(d.detailStatus()) + " · " + d.info.Profile
		if d.isLatestAttempt() && d.info.Phase != "" {
			status += " · " + d.info.Phase
		}
		footer = "↑↓ 滚动 · / 轮次 · p 过程 · Enter 详情 · Esc 返回"
		if d.showProcess && d.currentCursor() > 0 {
			status += fmt.Sprintf(" · 记录 %d–%d / %d", d.page.Before, d.page.End, d.page.Total)
		}
		if !d.follow && d.newRecords > 0 {
			status += fmt.Sprintf(" · 新增 %d 条", d.newRecords)
		}
		if d.newAttempt {
			status += " · 有新轮次，按 ] 查看"
		}
		if !d.historyLoaded && (d.loading || d.attemptsLoading) {
			status += " · 加载中"
		}
		processingView := ""
		processingHeight := 0
		listBodyHeight := bodyHeight
		showProcessing := d.active() && !d.showAttempts && d.inspector == nil && d.processing != nil
		if showProcessing {
			processingView = d.processing.RenderWithin(inner, max(1, bodyHeight-2))
			processingHeight = lipgloss.Height(processingView)
			if bodyHeight < 3 {
				listBodyHeight = 0
			} else {
				listBodyHeight = max(1, bodyHeight-processingHeight-1)
			}
		}
		if d.list != nil && !d.showAttempts && d.inspector == nil && d.list.Height() != listBodyHeight {
			anchor, line := d.list.ViewportAnchor()
			d.list.SetSize(inner, listBodyHeight)
			if d.follow {
				d.list.ScrollToBottom()
			} else {
				d.list.RestoreViewportAnchor(anchor, line)
			}
		}
		if d.showAttempts {
			title = "‹ 子任务 / 轮次"
			status = fmt.Sprintf("%d 个已保存轮次", len(d.attemptList()))
			if d.info.RunCount > len(d.attemptList()) {
				status += " · 含旧版记录"
			}
			body = m.taskAttemptsView(inner)
			footer = "↑↓ 选择 · Enter 查看 · / 收起 · Esc 返回"
		} else if d.inspector != nil {
			title = "‹ 子任务 / 工具详情"
			body = d.inspector.Render()
			footer = "↑↓ 滚动 · PgUp/PgDn 翻页 · Esc 返回"
			if output := d.output; output != nil {
				status = fmt.Sprintf("完整输出 · %d–%d / %d 字节", output.page.Offset, output.page.NextOffset, output.page.Total)
				if output.loading {
					status = "正在读取完整输出…"
				}
				if output.page.Incomplete {
					status += " · 保存内容不完整"
				}
				if output.page.HasMore {
					footer = "↑↓ 滚动 · PgDn 继续输出 · Esc 返回"
				}
				if output.err != "" {
					status = "输出不可用：" + output.err + " · r 重试"
				}
			}
		} else if d.list != nil {
			body = d.list.Render()
		}
		if showProcessing {
			if body != "" && listBodyHeight > 0 {
				body = strings.TrimRight(body, "\n") + "\n\n" + processingView
			} else {
				body = processingView
			}
		}
		if d.err != "" {
			status = "读取失败：" + d.err + " · r 重试"
		}
		if body == "" {
			if d.info.Status == "queued" && !d.info.Recorded {
				body = "正在排队，执行名额空出后自动开始。\n" + d.info.Prompt
			} else if d.info.Recorded {
				body = "正在加载执行记录…"
			} else {
				body = "尚无已保存的执行记录。\n" + d.info.Prompt
				if d.info.Error != "" {
					body += "\n" + d.info.Error
				}
			}
		}
	} else {
		status = fmt.Sprintf("%d 个任务 · Ctrl+G 关闭", s.listPage.Total)
		if s.query != "" || s.searching {
			status = "搜索：" + s.query
			if s.searching {
				status += "▏  Enter 完成"
			}
		}
		if s.listLoading && !s.listLoaded && len(s.items) == 0 {
			status += " · 加载中"
		}
		if s.listError != "" {
			status = "读取失败：" + s.listError + " · r 重试"
		}
		if len(s.items) == 0 {
			body = "当前会话还没有子任务。"
			if s.listLoading && !s.listLoaded {
				body = "正在加载子任务…"
			}
			if s.query != "" {
				body = "没有匹配的子任务。"
			}
		} else {
			selected := 0
			for i, item := range s.items {
				if item.ID == s.selected {
					selected = i
					break
				}
			}
			visible := max(1, bodyHeight)
			start := max(0, selected-visible+1)
			end := min(len(s.items), start+visible)
			var rows []string
			for i := start; i < end; i++ {
				item := s.items[i]
				prefix := "  "
				if item.ID == s.selected {
					prefix = "› "
				}
				status, label, statusWidth := taskStatus(item.Status), taskLabel(item), 12
				if inner < 45 {
					status = strings.Fields(status)[0]
					statusWidth = 2
					label = taskName(item)
				}
				line := prefix + lipgloss.NewStyle().Width(statusWidth).Render(status) + "  " + label
				if inner >= 100 && item.Phase != "" {
					line += " · " + item.Phase
				}
				if inner >= 45 {
					metrics := taskListMetrics(item, inner)
					if metrics != "" {
						separator := " · "
						available := inner - ansi.StringWidth(metrics) - ansi.StringWidth(separator)
						if available >= 16 {
							left := ansi.Truncate(line, available, "…")
							line = left + strings.Repeat(" ", max(0, available-ansi.StringWidth(left))) + separator + metrics
						}
					}
				}
				line = ansi.Truncate(ansi.Strip(line), inner, "…")
				if item.ID == s.selected {
					line = lipgloss.NewStyle().Foreground(lipgloss.Color("#e0e0e0")).Background(lipgloss.Color("#303a3a")).Width(inner).Render(line)
				}
				rows = append(rows, line)
			}
			body = strings.Join(rows, "\n")
		}
	}
	if inner < 50 {
		footer = "↑↓ 滚动 · Enter 查看 · Esc 返回"
	}
	if inner < 26 {
		footer = "↑↓ Enter Esc"
	}
	row := func(text string) string { return ansi.Truncate(text, inner, "…") }
	gap := "\n"
	if h >= 16 {
		gap = "\n\n"
	}
	content := sty.Teal.Bold(true).Render(row(ansi.Strip(title))) + "\n" + sty.Subtle.Render(row(ansi.Strip(status))) + gap
	content += fitTaskBody(body, inner, bodyHeight) + gap + sty.Subtle.Render(row(footer))
	style := lipgloss.NewStyle().Background(lipgloss.Color("#1b1e1e")).Width(w).Height(h)
	if w >= 32 && h >= 10 {
		// Use a complete edge-aligned set: its corner quadrants join both
		// adjoining strokes, while the fill remains inside the visible frame.
		style = style.Border(lipgloss.OuterHalfBlockBorder()).BorderForeground(lipgloss.Color("#5f9f9f")).Padding(0, 1)
	}
	return fillTaskBackground(style.Render(content), w, h)
}

// Nested message/Markdown styles can reset the terminal background. Fill only
// default-background cells after parsing those styles, retaining explicit
// backgrounds such as code blocks and the selected task row.
func fillTaskBackground(content string, width, height int) string {
	canvas := lipgloss.NewCanvas(width, height).Compose(lipgloss.NewLayer(content))
	background := lipgloss.Color("#1b1e1e")
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			cell := canvas.CellAt(x, y)
			if cell == nil || cell.Width == 0 {
				continue
			}
			filled := *cell
			if cell.Style.Bg == nil {
				filled.Style.Bg = background
			} else {
				continue
			}
			canvas.SetCell(x, y, &filled)
		}
	}
	return canvas.Render()
}

func taskContentSize(w, h int) (int, int) {
	spacing := 0
	if h >= 16 {
		spacing = 2
	}
	if w < 32 || h < 10 {
		return max(1, w), max(1, h-3-spacing)
	}
	return max(1, w-4), max(1, h-5-spacing)
}

func fitTaskBody(body string, width, height int) string {
	lines := strings.Split(body, "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	for i, line := range lines {
		lines[i] = ansi.Truncate(line, width, "…")
	}
	return strings.Join(lines, "\n")
}

func taskLabel(info controlruntime.SubagentSummary) string {
	id := info.ID
	if len(id) > 8 {
		id = id[:8]
	}
	return id + " · " + taskName(info)
}

func taskName(info controlruntime.SubagentSummary) string {
	name := strings.Join(strings.Fields(ansi.Strip(info.Title)), " ")
	if name == "" {
		name = info.Profile
	}
	if name == "" {
		name = "子任务"
	}
	return name
}

func (d *taskDetail) visibleRecords() []controlruntime.SubagentRecord {
	if d.attempt == nil {
		return d.page.Records
	}
	inside := func(sequence int) bool {
		return sequence >= d.attempt.TranscriptStart && sequence <= d.attempt.TranscriptEnd
	}
	records := make([]controlruntime.SubagentRecord, 0, len(d.page.Records))
	for i, record := range d.page.Records {
		if record.ContextOnly {
			if i+1 < len(d.page.Records) && inside(d.page.Records[i+1].Sequence) {
				records = append(records, record)
			}
			continue
		}
		if inside(record.Sequence) {
			records = append(records, record)
		}
	}
	return records
}

func formatTaskTokens(count int) string {
	if count < 1000 {
		return fmt.Sprint(count)
	}
	value, unit := float64(count)/1000, "K"
	if count >= 1_000_000 {
		value, unit = float64(count)/1_000_000, "M"
	}
	text := strings.TrimSuffix(fmt.Sprintf("%.1f", value), ".0")
	return text + unit
}

func taskMetrics(info controlruntime.SubagentSummary) string { return taskListMetrics(info, 100) }

func taskListMetrics(info controlruntime.SubagentSummary, width int) string {
	if info.RunCount == 0 {
		return "尚未执行"
	}
	parts := []string{fmt.Sprintf("轮次 %d", info.RunCount)}
	attempt := info.LatestAttempt
	if attempt == nil {
		if width >= 100 {
			parts = append(parts, "旧版无明细")
		}
		return strings.Join(parts, " · ")
	}
	if total := attempt.CacheHitTokens + attempt.CacheMissTokens; total > 0 {
		parts = append(parts, fmt.Sprintf("缓存 %.0f%%", float64(attempt.CacheHitTokens)*100/float64(total)))
	}
	if width >= 100 {
		if attempt.ContextWindow > 0 {
			parts = append(parts, "上下文 "+formatTaskTokens(attempt.ContextTokens)+"/"+formatTaskTokens(attempt.ContextWindow))
		} else if attempt.ContextTokens > 0 {
			parts = append(parts, "上下文 "+formatTaskTokens(attempt.ContextTokens))
		}
	}
	parts = append(parts, fmt.Sprintf("工具 %d", attempt.ToolUseCount))
	return strings.Join(parts, " · ")
}

func (m *Model) rebuildTaskDetail() {
	d := m.tasks.detail
	if d == nil {
		return
	}
	sty := styles.DefaultStyles()
	anchor, line := d.list.ViewportAnchor()
	anchorKey := ""
	if anchor < len(d.entries) {
		anchorKey = d.entries[anchor].key
	}
	records := d.visibleRecords()
	results := make(map[string]controlruntime.SubagentRecord, len(d.committedResults)+len(records))
	for id, result := range d.committedResults {
		results[id] = result
	}
	calls := make(map[string]controlruntime.SubagentToolCall)
	callSequences := make(map[string]int)
	for _, record := range records {
		if record.ToolCallID != "" {
			results[record.ToolCallID] = record
		}
		for _, call := range record.ToolCalls {
			calls[call.ID] = call
			callSequences[call.ID] = record.Sequence
		}
	}
	var entries []taskEntry
	prefix := "legacy:"
	if d.attempt != nil {
		prefix = "attempt:" + d.attempt.ID + ":"
	}
	previous := make(map[string]taskEntry, len(d.entries))
	for _, entry := range d.entries {
		previous[entry.key] = entry
	}
	add := func(key string, state any, create func() components.Item) {
		key = prefix + key
		entry := taskEntry{key: key, renderState: state}
		if old, ok := previous[key]; ok && reflect.DeepEqual(old.renderState, state) {
			entry.item = old.item
		} else {
			entry.item = create()
		}
		entries = append(entries, entry)
	}
	addText := func(key, role, text, reasoning string) {
		state := struct{ Role, Text, Reasoning string }{role, text, reasoning}
		add(key, state, func() components.Item {
			switch role {
			case "user":
				return message.NewUserMessageItem(&sty, text)
			case "system":
				return message.NewSystemMessageItem(&sty, text)
			default:
				item := message.NewAssistantMessageItem(&sty, text)
				item.SetReasoning(reasoning)
				return item
			}
		})
	}
	makeBlock := func(call controlruntime.SubagentToolCall) (block.ContentBlock, controlruntime.SubagentRecord) {
		result, done := results[call.ID]
		if live, ok := d.tools[call.ID]; ok && !done {
			result = controlruntime.SubagentRecord{Content: live.payload.Preview, ToolCallID: call.ID}
			if live.done {
				result.Content = live.payload.Output
				result.IsError = live.payload.IsError
				done = true
			}
		}
		content := result.Content
		if !done && content == "" {
			content = "执行中…"
			if d.detailStatus() != "running" && !strings.HasPrefix(d.detailStatus(), "waiting_") {
				end := d.info.TranscriptSeq
				if d.attempt != nil {
					end = d.attempt.TranscriptEnd
				}
				content = "结果在后续记录中"
				if callSequences[call.ID] >= end {
					content = "未记录到工具结果"
				}
			}
		}
		if result.OutputPath != "" {
			content += "\n完整输出：" + result.OutputPath
		}
		tool := block.NewTool(call.Name, call.ID, call.Arguments)
		tool.Content, tool.Done, tool.IsError = content, done, result.IsError
		return tool, result
	}
	addTool := func(call controlruntime.SubagentToolCall) {
		if call.Name == "nekocode_submit_result" {
			addText("result:"+call.ID, "assistant", taskResultText(call.Arguments), "")
			return
		}
		toolBlock, result := makeBlock(call)
		add("tool:"+call.ID, toolBlock, func() components.Item {
			item := message.NewAssistantMessageItem(&sty, "")
			item.SetBlocks([]block.ContentBlock{toolBlock})
			return item
		})
		copyCall := call
		entries[len(entries)-1].call, entries[len(entries)-1].result = &copyCall, result
	}
	if d.showProcess {
		for _, record := range records {
			key := fmt.Sprintf("record:%d", record.Sequence)
			switch record.Role {
			case "user":
				if !record.ContextOnly {
					if record.RuntimeEvent {
						content := record.Content
						if strings.HasPrefix(content, "[Sub-agent completion protocol]") {
							content = "任务结果需要补全，正在继续执行。"
						}
						addText(key, "system", content, "")
					} else {
						addText(key, "user", strings.TrimPrefix(record.Content, "[Current delegated task]\n"), "")
					}
				}
			case "assistant":
				content, reasoning := processing.DisplayText(record.Content), processing.DisplayText(record.Reasoning)
				if !record.ContextOnly && (content != "" || reasoning != "") {
					addText(key, "assistant", content, reasoning)
				}
				for _, call := range record.ToolCalls {
					if record.ContextOnly {
						if _, ok := results[call.ID]; !ok {
							continue
						}
					}
					addTool(call)
				}
			case "system":
				if !record.ContextOnly {
					addText(key, "system", record.Content, "")
				}
			}
		}
		var liveIDs []string
		for id, live := range d.tools {
			if _, ok := calls[id]; !ok && !live.done {
				liveIDs = append(liveIDs, id)
			}
		}
		sort.Strings(liveIDs)
		for _, id := range liveIDs {
			live := d.tools[id]
			addTool(controlruntime.SubagentToolCall{ID: id, Name: live.payload.ToolName, Arguments: live.payload.Args})
		}
	} else {
		status := d.detailStatus()
		prompt := d.info.Prompt
		if d.attempt != nil {
			prompt = d.attempt.Prompt
		}
		if prompt != "" {
			addText("delegated-prompt", "user", prompt, "")
		}
		if !d.active() {
			result := ""
			for _, record := range records {
				for _, call := range record.ToolCalls {
					if call.Name == "nekocode_submit_result" {
						result = taskResultText(call.Arguments)
					}
				}
			}
			if result == "" && d.loading {
				result = "正在加载完整结果…"
			}
			if result == "" {
				result = d.info.Result
				if d.attempt != nil {
					result = d.attempt.Result
				}
			}
			if result == "" {
				result = d.info.Error
			}
			if result == "" {
				result = taskStatus(status) + "，按 p 查看本次过程。"
			}
			addText("final", "assistant", result, "")
		}
	}
	if d.atLatestTail() && (d.showProcess || !d.active()) {
		if d.follow || d.inspector != nil {
			d.livePreview = nil
			if live := d.liveMessage; live != nil {
				content, reasoning := processing.DisplayText(live.text.String()), processing.DisplayText(live.reasoning.String())
				if content != "" || reasoning != "" {
					addText(fmt.Sprintf("record:%d", live.sequence), "assistant", content, reasoning)
					d.livePreview = &taskLivePreview{sequence: live.sequence, entry: entries[len(entries)-1]}
				}
			}
		} else if d.livePreview != nil {
			// Paused reading freezes the displayed preview, while new deltas
			// continue accumulating separately until following resumes.
			entries = append(entries, d.livePreview.entry)
		}
	}
	if d.active() {
		if d.processing == nil {
			d.processing = processing.NewProcessingItem(&sty)
		}
		p := d.processing
		output, thinking := p.OutputText(), p.ThinkingText()
		if d.follow || d.inspector != nil {
			output, thinking = taskProcessingText(records, d.liveMessage)
		}
		summary := "当前执行"
		if d.attempt != nil {
			summary = fmt.Sprintf("第 %d 轮", d.attempt.Seq)
		}
		p.SetSubagentSummary(summary)
		phase := taskStatus(d.detailStatus())
		if d.isLatestAttempt() && d.info.Phase != "" {
			phase = d.info.Phase
		}
		p.SetStatusText(phase)
		toolCalls := d.processingToolCalls(records)
		blocks := make([]block.ContentBlock, 0, len(toolCalls))
		for _, call := range toolCalls {
			if call.Name == "nekocode_submit_result" {
				continue
			}
			toolBlock, _ := makeBlock(call)
			blocks = append(blocks, toolBlock)
		}
		if !reflect.DeepEqual(p.Blocks(), blocks) {
			p.SetBlocks(blocks)
		}
		p.SetStreamText(output, thinking)
		p.SetTodos(taskProcessingTodos(records, results))
		p.SetSkill(strings.Join(d.info.Skills, ", "))
	} else {
		d.processing = nil
	}
	items := make([]components.Item, len(entries))
	for i, entry := range entries {
		items[i] = entry.item
	}
	// Keep cached renders and offsets when the page structure is stable.
	stable := len(entries) >= len(d.entries)
	for i := 0; stable && i < len(d.entries); i++ {
		stable = entries[i].key == d.entries[i].key
	}
	if stable {
		for i, old := range d.entries {
			if old.item != entries[i].item {
				d.list.ReplaceItem(i, entries[i].item)
			}
		}
		d.list.AppendItems(items[len(d.entries):]...)
	} else {
		d.list.SetItems(items...)
	}
	d.entries = entries
	d.list.SetGap(1)
	m.resizeTasks()
	if d.follow {
		d.list.ScrollToBottom()
	} else {
		for i, entry := range entries {
			if entry.key == anchorKey {
				anchor = i
				break
			}
		}
		d.list.RestoreViewportAnchor(anchor, line)
	}
}

func taskResultText(arguments string) string {
	var result struct {
		Summary      string   `json:"summary"`
		Evidence     []string `json:"evidence"`
		Files        []string `json:"files"`
		Verification string   `json:"verification"`
		Unfinished   []string `json:"unfinished"`
		Risks        []string `json:"risks"`
	}
	if json.Unmarshal([]byte(arguments), &result) != nil {
		return arguments
	}
	var out strings.Builder
	out.WriteString("**结果**\n\n" + result.Summary)
	if result.Verification != "" {
		out.WriteString("\n\n**验证**\n\n" + result.Verification)
	}
	for _, section := range []struct {
		name  string
		items []string
	}{{"证据", result.Evidence}, {"文件", result.Files}, {"未完成", result.Unfinished}, {"风险", result.Risks}} {
		if len(section.items) > 0 {
			out.WriteString("\n\n**" + section.name + "**\n")
			for _, item := range section.items {
				out.WriteString("\n- " + item)
			}
		}
	}
	return out.String()
}

func taskToolSnapshot(d *taskDetail, call controlruntime.SubagentToolCall) (controlruntime.SubagentToolCall, controlruntime.SubagentRecord) {
	result := d.committedResults[call.ID]
	savedCall := false
	for _, record := range d.visibleRecords() {
		for _, saved := range record.ToolCalls {
			if saved.ID == call.ID {
				call = saved
				savedCall = true
			}
		}
		if record.ToolCallID == call.ID {
			result = record
		}
	}
	if live, ok := d.tools[call.ID]; ok {
		if !savedCall && live.payload.Args != "" {
			call.Arguments = live.payload.Args
		}
		if live.done {
			result.Content = live.payload.Output
			result.IsError = live.payload.IsError
		} else if result.Content == "" {
			result.Content = live.payload.Preview
		}
	}
	return call, result
}

func (m *Model) inspectVisibleTaskTool() tea.Cmd {
	d := m.tasks.detail
	if d == nil {
		return nil
	}
	index, skipped := d.list.ViewportAnchor()
	// Enter targets the first visible tool, rather than an off-screen call.
	remaining := d.list.Height() + skipped
	for i := index; i < len(d.entries) && remaining > 0; i++ {
		entry := d.entries[i]
		if entry.call != nil {
			return m.inspectTaskTool(entry)
		}
		remaining -= lipgloss.Height(entry.item.Render(d.list.Width())) + 1
	}
	if d.processing != nil {
		records := d.visibleRecords()
		hasOutput := func(id string) bool {
			if d.committedResults[id].OutputPath != "" {
				return true
			}
			for _, record := range records {
				if record.ToolCallID == id && record.OutputPath != "" {
					return true
				}
			}
			return false
		}
		calls := d.processingToolCalls(records)
		candidate := -1
		for i, call := range calls {
			candidate = i
			if hasOutput(call.ID) {
				break
			}
		}
		if candidate >= 0 {
			call, result := taskToolSnapshot(d, calls[candidate])
			return m.inspectTaskTool(taskEntry{key: "processing:tool:" + call.ID, call: &call, result: result})
		}
	}
	return nil
}

func (m *Model) inspectTaskTool(entry taskEntry) tea.Cmd {
	d := m.tasks.detail
	d.inspectedEntry = &entry
	d.inspectedKey = entry.key
	d.output = nil
	m.renderTaskInspector()
	if entry.result.OutputPath != "" && entry.result.Sequence > 0 {
		if _, ok := m.Runtime.(controlruntime.SubagentOutputReader); ok {
			d.output = &taskOutputWindow{sequence: entry.result.Sequence, offsets: []int64{0}}
			return m.loadTaskOutput(0)
		}
	}
	return nil
}
