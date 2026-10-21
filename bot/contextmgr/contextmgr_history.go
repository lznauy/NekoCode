package contextmgr

import (
	"strings"
	"unicode/utf8"

	"nekocode/bot/contextmgr/token"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/provider/types"
	"nekocode/logger"
)

type ToolResultMsg struct {
	Message         types.Message
	ToolName        string
	AllowOutputFile func(string) error
}

func (m *Manager) Add(role, content string, source ...string) {
	s := ""
	if len(source) > 0 {
		s = source[0]
	}
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	message := types.Message{Role: role, Content: content, Source: s}
	m.state.ctx.Messages = append(m.state.ctx.Messages, message)
	m.state.transcript = append(m.state.transcript, message)
	m.state.tracker.AddNew(len(role) + len(content))
	m.state.revision++
}

// AddUser appends a user message carrying native image attachments. The
// images ride on the message (and persist with the session). Text is charged
// as characters (AddNew applies the chars→tokens conversion) while image
// estimates are already token counts and must bypass that conversion —
// mixing them would undercharge images by 4x and delay compaction until the
// request overflows. Image tokens are charged only while the active model
// actually receives image input, so switching models re-prices history
// correctly.
func (m *Manager) AddUser(content string, images []types.MessageImage) {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	message := types.Message{Role: "user", Content: content, Source: "user", Images: images}
	m.state.ctx.Messages = append(m.state.ctx.Messages, message)
	m.state.transcript = append(m.state.transcript, message)
	m.state.tracker.AddNew(len("user") + len(content))
	if m.state.vision {
		imageTokens := 0
		for _, image := range images {
			imageTokens += token.EstimateImageTokens(image)
		}
		if imageTokens > 0 {
			m.state.tracker.AddEstimated(imageTokens)
		}
	}
	m.state.revision++
}

// AddAssistant persists the complete assistant message while charging only the
// reasoning that the active model contract will replay on its next request.
func (m *Manager) AddAssistant(message types.Message) {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	message.Role = "assistant"
	m.state.ctx.Messages = append(m.state.ctx.Messages, message)
	m.state.transcript = append(m.state.transcript, message)
	m.state.tracker.AddEstimated(token.EstimateModelTokens([]types.Message{message}, m.state.reasoning, m.state.vision))
	m.state.revision++
}

func (m *Manager) AddToolResultsBatch(results []ToolResultMsg) {
	m.state.mu.Lock()
	var loadedSkills []string
	defer func() {
		m.state.mu.Unlock()
		m.recordSkillsLoaded(loadedSkills)
	}()
	// One shared budget for the whole batch. Leave half the remaining space
	// for request overhead, assistant output and the next compaction. Reserve
	// message metadata separately; content bytes conservatively bound tokens.
	window := m.state.contextWindow
	if window <= 0 {
		window = defaultBudget
	}
	remaining := max(0, (window-m.estimatedTokens()-window/10)/2)
	for _, r := range results {
		remaining = max(0, remaining-token.EstimateString(r.Message.ToolCallID)-8)
	}
	for i, r := range results {
		role := "tool"
		if r.Message.ToolCallID == "" {
			role = "user"
		}
		output := core.ToolOutput{Text: r.Message.Content, Artifact: r.Message.ToolOutput}
		limit := min(core.MaxOutputBytes, remaining/(len(results)-i))
		deferred := false
		if len(output.Render()) > limit {
			if r.ToolName == "skill" || limit < 512 {
				deferred = true
			} else {
				bounded, err := m.state.outputStore.BoundOutput(output, limit)
				if err == nil && r.AllowOutputFile != nil {
					err = r.AllowOutputFile(bounded.Artifact.Path)
				}
				if err != nil {
					deferred = true
				} else {
					output = bounded
				}
			}
		}
		message := types.Message{Role: role, ToolCallID: r.Message.ToolCallID, IsError: r.Message.IsError}
		if r.ToolName == "skill" && !message.IsError {
			message.SkillName = r.Message.SkillName
		}
		if deferred {
			var err error
			// Preserve the exact bounded delivery, including errors and capture
			// status, rather than reconstructing it from the raw artifact.
			pending := output
			if len(output.Render()) > core.MaxOutputBytes {
				pending, err = m.state.outputStore.BoundOutput(output, core.MaxOutputBytes)
				if err == nil {
					output = pending
				}
			}
			if err == nil {
				message.DeferredContent = pending.Render()
			}
			if err == nil && output.Artifact.Path == "" {
				output.Artifact.Path, err = m.state.outputStore.PreserveOutput(output.Text)
			}
			if err == nil {
				err = m.state.outputStore.CheckArtifact(output.Artifact)
			}
			if err == nil && r.AllowOutputFile != nil {
				err = r.AllowOutputFile(output.Artifact.Path)
			}
			message.Content = core.DeferredOutput
			if err == nil {
				message.ToolOutput = output.Artifact
				message.Content += "\nFull result: " + output.Artifact.Path
			} else {
				message.DeferredContent = ""
				message.Content += "\nFull result could not be saved."
			}
			message.Source = core.DeferredOutputSource
		} else {
			message.Content, message.ToolOutput = output.Render(), output.Artifact
			if message.SkillName != "" {
				loadedSkills = append(loadedSkills, message.SkillName)
			}
		}
		remaining = max(0, remaining-len(message.Content))
		m.state.ctx.Messages = append(m.state.ctx.Messages, message)
		transcriptMessage := message
		transcriptMessage.Source = ""
		transcriptMessage.DeferredContent = ""
		// The transcript keeps the untruncated original, but bounded: one
		// giant tool result (e.g. reading a huge file) must not exceed the
		// transcript reader's per-line limit and make the session unloadable.
		transcriptMessage.Content = capTranscriptContent(core.ToolOutput{Text: r.Message.Content, Artifact: r.Message.ToolOutput}.Render())
		m.state.transcript = append(m.state.transcript, transcriptMessage)
		m.state.tracker.AddNew(len(role) + len(message.Content) + len(r.Message.ToolCallID))
	}
	if len(results) > 0 {
		m.state.revision++
	}
}

// transcriptContentLimit bounds a single transcript record. It sits far above
// the active-context budget (so the transcript stays a faithful record) and
// far below the reader's 16 MiB line limit (so loading can never fail).
const transcriptContentLimit = 4 << 20

func capTranscriptContent(content string) string {
	if len(content) <= transcriptContentLimit {
		return content
	}
	end := transcriptContentLimit
	// Back off so the cut never lands inside a multi-byte rune: invalid UTF-8
	// would be mangled into U+FFFD when the transcript is re-serialized. The
	// cut is clean when the first excluded byte starts a new rune.
	for end > 0 && !utf8.RuneStart(content[end]) {
		end--
	}
	truncated := content[:end]
	if idx := strings.LastIndexByte(truncated, '\n'); idx > 0 {
		truncated = truncated[:idx]
	}
	logger.Log("transcript: capped tool result from %d to %d bytes", len(content), len(truncated))
	return truncated + "\n... [transcript record capped at 4 MiB]"
}

// Reset clears both active history and its compaction archive.
func (m *Manager) Reset() {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	m.clearLocked()
	m.state.ctx.Archive = ""
	m.state.transcript = nil
	m.state.ctx.Hints = ""
	m.state.runtimePolicy = ""
	m.state.tracker.Restore(token.State{})
	m.state.prefix.Reset()
	m.resetRuntimeContextLocked()
	m.state.compactCount = 0
	m.state.trimCount = 0
	m.state.revision++
}

func (m *Manager) TruncateTo(n int) {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	if n < 0 {
		n = 0
	}
	if n < len(m.state.ctx.Messages) {
		logger.Log("truncate_to: dropped %d messages (kept %d, was %d)", len(m.state.ctx.Messages)-n, n, len(m.state.ctx.Messages))
		m.state.ctx.Messages = m.state.ctx.Messages[:n]
		m.state.revision++
		m.state.tracker.RecordPrompt(m.totalTokenEstimate())
		m.restoreRuntimeContextLocked()
	}
}

func (m *Manager) RemoveMessages(startIdx, endIdx int) {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	if startIdx < 0 || endIdx >= len(m.state.ctx.Messages) || startIdx > endIdx {
		return
	}
	n := endIdx - startIdx + 1
	m.state.ctx.Messages = append(m.state.ctx.Messages[:startIdx], m.state.ctx.Messages[endIdx+1:]...)
	m.state.revision++
	m.state.tracker.RecordPrompt(m.totalTokenEstimate())
	m.restoreRuntimeContextLocked()
	logger.Log("remove_messages: dropped %d messages [%d:%d] (total now %d)", n, startIdx, endIdx, len(m.state.ctx.Messages))
}

func (m *Manager) clearLocked() {
	m.state.ctx.Messages = make([]types.Message, 0)
	m.state.ctx.TodoItems = nil
}
