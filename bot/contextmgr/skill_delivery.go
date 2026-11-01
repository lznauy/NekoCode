package contextmgr

import "nekocode/bot/provider/types"

func skillAdmission(name, source, content string, sequence int) types.SkillDelivery {
	state := types.SkillReady
	if content == "" {
		state = types.SkillIncomplete
	}
	return types.SkillDelivery{Schema: 1, Name: name, Source: source, State: state, ContentHash: types.SkillContentHash(content), OriginSequence: sequence}
}

// AddSkillWorkflow preserves explicit identity without parsing workflow text.
func (m *Manager) AddSkillWorkflow(name, content, source string) {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	message := types.Message{Role: "user", Content: content, Source: types.MessageSourceSkillWorkflow, SkillName: name,
		SkillDelivery: skillAdmission(name, source, content, m.state.transcriptBase+len(m.state.transcript)+1)}
	m.addMessageLocked(message)
}

// RecordSkillWorkflows records request-scoped delegated workflows after request
// admission. Bodies stay with the request; this appends only local metadata.
func (m *Manager) RecordSkillWorkflows(names, contents []string) {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	for i, content := range contents {
		name := ""
		if i < len(names) {
			name = names[i]
		}
		delivery := skillAdmission(name, "delegated", content, m.state.transcriptBase+len(m.state.transcript)+1)
		m.appendSkillAdmissionLocked(delivery)
	}
}

func (m *Manager) appendSkillAdmissionLocked(delivery types.SkillDelivery) {
	m.state.transcript = append(m.state.transcript, types.Message{Role: "user", Source: types.MessageSourceRuntimeEvent, SkillDelivery: delivery})
	// Transcript metadata is deliberately absent from active model context.
	m.state.revision++
}
