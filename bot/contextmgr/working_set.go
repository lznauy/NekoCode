package contextmgr

import (
	"strings"

	"nekocode/bot/extension/tool/runtime/core"
)

const (
	workingSetRecentBatches = 4
	workingSetHighBytes     = 4 * core.MaxOutputBytes
	workingSetLowBytes      = 2 * core.MaxOutputBytes
	workingSetPreviewBytes  = 1024
)

// checkpointWorkingSet shrinks already archived retrieval output only at a
// pressure checkpoint. Between checkpoints, history remains append-only for
// prefix caching. Transcript, call/result pairing, errors and recent batches
// are never discarded. This is not a semantic summary or a success verdict.
func (m *Manager) checkpointWorkingSet() (changed bool) {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	if m.state.outputStore == nil {
		return
	}
	history := m.state.ctx.Messages
	// Only uniquely paired, fully settled batches are eligible. Keep the last
	// four intact, plus every incomplete/ambiguous batch regardless of age.
	type callState struct {
		name                        string
		resultIndex, calls, results int
		settled                     bool
	}
	calls := make(map[string]*callState)
	for i, msg := range history {
		if msg.Role == "assistant" {
			for _, call := range msg.ToolCalls {
				state := calls[call.ID]
				if state == nil {
					state = &callState{}
					calls[call.ID] = state
				}
				state.name = call.Function.Name
				state.calls++
			}
		}
		if msg.Role == "tool" {
			state := calls[msg.ToolCallID]
			if state == nil {
				state = &callState{}
				calls[msg.ToolCallID] = state
			}
			state.resultIndex = i
			state.results++
		}
	}
	var batches []int
	for i, msg := range history {
		if msg.Role != "assistant" || len(msg.ToolCalls) == 0 {
			continue
		}
		settled := true
		for _, call := range msg.ToolCalls {
			state := calls[call.ID]
			settled = settled && call.ID != "" && state.calls == 1 && state.results == 1 && state.resultIndex > i
		}
		if !settled {
			continue
		}
		batches = append(batches, i)
		for _, call := range msg.ToolCalls {
			calls[call.ID].settled = true
		}
	}
	if len(batches) <= workingSetRecentBatches {
		return
	}
	cutoff := batches[len(batches)-workingSetRecentBatches]
	candidates := make([]int, 0)
	size := 0
	for i, msg := range history {
		if i >= cutoff {
			break
		}
		call := calls[msg.ToolCallID]
		if call == nil || !call.settled || call.resultIndex != i {
			continue
		}
		switch call.name {
		case "read", "grep", "glob", "tree", "list":
		default:
			continue
		}
		if msg.Role != "tool" || msg.IsError || msg.Source != "" || msg.DeferredContent != "" || len(msg.Images) > 0 || msg.SkillName != "" {
			continue
		}
		if msg.ToolOutput.Path == "" || msg.ToolOutput.Incomplete || len(msg.Content) <= 2*workingSetPreviewBytes {
			continue
		}
		if m.state.outputStore.CheckArtifact(msg.ToolOutput) != nil {
			continue
		}
		candidates = append(candidates, i)
		size += len(msg.Content)
	}
	if size <= workingSetHighBytes {
		return
	}
	// Preserve known provider framing/tokenization overhead while removing only
	// the locally estimated cost of content actually narrowed below.
	calibration := max(0, m.state.tracker.PromptEstimate()-m.totalTokenEstimate())
	for _, i := range candidates {
		if size <= workingSetLowBytes {
			break
		}
		msg := &m.state.ctx.Messages[i]
		header := core.OutputReferenceHeader(msg.ToolOutput.Path, true)
		body := strings.TrimPrefix(msg.Content, header)
		output, err := m.state.outputStore.BoundOutput(core.ToolOutput{Text: body, Artifact: msg.ToolOutput}, workingSetPreviewBytes)
		if err != nil {
			continue
		}
		next := output.Render()
		if len(next) >= len(msg.Content) {
			continue
		}
		size -= len(msg.Content) - len(next)
		msg.Content = next
		// A shortened preview cannot authorize edits to the original source file.
		msg.ReadID = ""
		changed = true
	}
	if changed {
		m.state.revision++
		m.state.tracker.RecordPrompt(m.totalTokenEstimate() + calibration)
	}
	return changed
}
