package sessionstate

import (
	"sort"

	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/session"
)

func ApplyContextSnapshot(sess *session.Snapshot, snap ctxmgr.ManagerSnapshot, promptTokens, completionTokens int, loaded map[string]bool) {
	if sess == nil {
		return
	}
	sess.SystemPrompt = snap.SystemPrompt
	sess.Skills = snap.Skills
	sess.Memory = snap.Memory
	sess.Archive = snap.Archive
	sess.Messages = snap.Messages
	sess.CompactBoundary = snap.CompactBoundary
	sess.ContextWindow = snap.Budget
	sess.PromptTokens = promptTokens
	sess.CompletionTokens = completionTokens
	sess.LoadedSkills = LoadedSkillNames(loaded)
}

func ManagerSnapshot(sess *session.Snapshot) ctxmgr.ManagerSnapshot {
	if sess == nil {
		return ctxmgr.ManagerSnapshot{}
	}
	return ctxmgr.ManagerSnapshot{
		SystemPrompt:    sess.SystemPrompt,
		Skills:          sess.Skills,
		Archive:         sess.Archive,
		Memory:          sess.Memory,
		CompactBoundary: sess.CompactBoundary,
		Messages:        sess.Messages,
		Budget:          sess.ContextWindow,
	}
}

func LoadedSkillNames(loaded map[string]bool) []string {
	names := make([]string, 0, len(loaded))
	for name, ok := range loaded {
		if ok {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}
