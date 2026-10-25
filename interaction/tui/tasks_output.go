package tui

import (
	"bytes"
	"encoding/json"

	"nekocode/interaction/tui/components"
	"nekocode/interaction/tui/components/block"
	"nekocode/interaction/tui/components/message"
	"nekocode/interaction/tui/styles"
	controlruntime "nekocode/runtime"

	tea "charm.land/bubbletea/v2"
)

type taskOutputWindow struct {
	sequence        int
	request         uint64
	page            controlruntime.SubagentOutputPage
	offsets         []int64
	loading         bool
	loaded          bool
	resume          bool
	requestedOffset int64
	err             string
}

type taskOutputMsg struct {
	generation, request uint64
	id, key             string
	page                controlruntime.SubagentOutputPage
	err                 error
}

func (m *Model) loadTaskOutput(offset int64) tea.Cmd {
	s, d := m.tasks, m.tasks.detail
	reader, ok := m.Runtime.(controlruntime.SubagentOutputReader)
	if !ok || d == nil || d.output == nil {
		return nil
	}
	out := d.output
	d.outputRequest++
	out.request = d.outputRequest
	out.loading = true
	out.resume = false
	out.requestedOffset = offset
	generation, request, parent, id, key, sequence := s.generation, out.request, s.sessionID, d.id, d.inspectedKey, out.sequence
	return func() tea.Msg {
		page, err := reader.SubagentToolOutput(parent, id, sequence, offset)
		return taskOutputMsg{generation, request, id, key, page, err}
	}
}

func (m *Model) handleTaskOutputMessage(msg tea.Msg) (bool, tea.Cmd) {
	response, ok := msg.(taskOutputMsg)
	if !ok {
		return false, nil
	}
	s := m.tasks
	d := s.detail
	if !s.open || response.generation != s.generation || d == nil || d.id != response.id || d.inspector == nil || d.inspectedKey != response.key || d.output == nil || response.request != d.output.request {
		return true, nil
	}
	out := d.output
	out.loading = false
	if response.err != nil {
		out.err = response.err.Error()
		return true, nil
	}
	previous := out.page.Offset
	found := -1
	for i, offset := range out.offsets {
		if offset == response.page.Offset {
			found = i
			break
		}
	}
	if found >= 0 {
		out.offsets = out.offsets[:found+1]
	} else {
		out.offsets = append(out.offsets, response.page.Offset)
	}
	out.err = ""
	out.loaded = true
	out.page = response.page
	m.renderTaskInspector()
	if response.page.Offset < previous {
		d.inspector.ScrollToBottom()
	}
	return true, nil
}

func (m *Model) renderTaskInspector() {
	d := m.tasks.detail
	if d == nil || d.inspectedEntry == nil {
		return
	}
	entry := d.inspectedEntry
	sty := styles.DefaultStyles()
	var items []components.Item
	if d.output == nil || d.output.page.Offset == 0 {
		args := entry.call.Arguments
		var pretty bytes.Buffer
		if json.Indent(&pretty, []byte(args), "", "  ") == nil {
			args = pretty.String()
		}
		parameters := message.NewAssistantMessageItem(&sty, "")
		parameters.SetBlocks([]block.ContentBlock{{Type: block.BlockTool, ToolName: "参数", Content: args, Done: true, FullOutput: true}})
		items = append(items, parameters)
	}
	content := entry.result.Content
	if d.output != nil && d.output.loaded {
		content = d.output.page.Content
	}
	tool := message.NewAssistantMessageItem(&sty, "")
	toolBlock := block.NewTool(entry.call.Name, entry.call.ID, entry.call.Arguments)
	toolBlock.Content, toolBlock.Done, toolBlock.IsError, toolBlock.FullOutput = content, true, entry.result.IsError, true
	tool.SetBlocks([]block.ContentBlock{toolBlock})
	items = append(items, tool)
	d.inspector = components.NewList(items...)
	d.inspector.SetGap(1)
	m.resizeTasks()
}

func (m *Model) refreshTaskInspector() tea.Cmd {
	d := m.tasks.detail
	if d == nil || d.inspector == nil || d.inspectedEntry == nil || d.output != nil {
		return nil
	}
	old := d.inspectedEntry
	next := *old
	if old.call != nil {
		call, result := taskToolSnapshot(d, *old.call)
		next.call, next.result = &call, result
	}
	for _, entry := range d.entries {
		if entry.key == d.inspectedKey {
			next = entry
			break
		}
	}
	if old.result.Content != next.result.Content || old.result.IsError != next.result.IsError || old.result.OutputPath != next.result.OutputPath || old.result.Sequence != next.result.Sequence || old.call.Arguments != next.call.Arguments {
		index, line := d.inspector.ViewportAnchor()
		d.inspectedEntry = &next
		m.renderTaskInspector()
		d.inspector.RestoreViewportAnchor(index, line)
	}
	if next.result.OutputPath != "" && next.result.Sequence > 0 {
		if _, ok := m.Runtime.(controlruntime.SubagentOutputReader); ok {
			d.output = &taskOutputWindow{sequence: next.result.Sequence, offsets: []int64{0}}
			return m.loadTaskOutput(0)
		}
	}
	return nil
}
