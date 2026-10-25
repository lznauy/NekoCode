package tui

import (
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	controlruntime "nekocode/runtime"
)

const taskFrameInterval = time.Second / 30
const taskLiveTextBytes = 64 << 10

type taskFrameMsg struct {
	generation, request uint64
	id                  string
}

type taskLiveMessage struct {
	sequence        int
	text, reasoning strings.Builder
}

type taskLivePreview struct {
	sequence int
	entry    taskEntry
}

// Live text is a bounded preview; full messages come from the transcript.
func appendTaskLiveText(dst *strings.Builder, text string) {
	if dst.Len()+len(text) <= taskLiveTextBytes {
		dst.WriteString(text)
		return
	}
	text = dst.String() + text
	start := len(text) - taskLiveTextBytes
	for start < len(text) && !utf8.RuneStart(text[start]) {
		start++
	}
	dst.Reset()
	dst.WriteString(text[start:])
}

func (d *taskDetail) applyLiveOutput(ev controlruntime.Event, output controlruntime.SubAgentOutput) {
	if output.ID != d.id || !d.isLatestAttempt() || !d.acceptsAttempt(output.AttemptID) || output.MessageSeq <= d.page.Total || output.MessageSeq < 1 {
		return
	}
	if ev.Sequence > 0 && ev.Sequence <= d.eventSeq {
		return
	}
	if d.liveMessage != nil && output.MessageSeq < d.liveMessage.sequence {
		return
	}
	d.eventSeq = max(d.eventSeq, ev.Sequence)
	if d.liveMessage == nil || d.liveMessage.sequence != output.MessageSeq || output.Kind == "reset" {
		d.liveMessage = &taskLiveMessage{sequence: output.MessageSeq}
	}
	live := d.liveMessage
	switch output.Kind {
	case "text_delta":
		appendTaskLiveText(&live.text, output.Text)
	case "reasoning_delta":
		appendTaskLiveText(&live.reasoning, output.Text)
	case "message":
		live.text.Reset()
		live.reasoning.Reset()
		appendTaskLiveText(&live.text, output.Text)
		appendTaskLiveText(&live.reasoning, output.Reasoning)
	}
	d.dirty = true
}

func (m *Model) scheduleTaskFrame() tea.Cmd {
	s := m.tasks
	d := s.detail
	if d == nil || d.info.Recorded && !d.historyLoaded || !d.dirty || d.framePending || !d.atLatestTail() || (!d.follow && d.inspector == nil) {
		return nil
	}
	d.framePending = true
	msg := taskFrameMsg{generation: s.generation, request: s.streamRequest, id: d.id}
	return tea.Tick(taskFrameInterval, func(time.Time) tea.Msg { return msg })
}
