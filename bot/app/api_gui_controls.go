package app

import (
	"fmt"

	"nekocode/bot/command"
	"nekocode/common"
)

func (b *Bot) ContextStatus() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return command.ContextStats(b.ctxMgr)
}

func (b *Bot) ContextReport() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	report := command.ContextReport(b.ctxMgr, b.toolRegistry.Descriptors())
	if b.ag != nil {
		if gov := b.ag.GovernanceLine(); gov != "" {
			report += "\n\n" + gov
		}
	}
	return report
}

func (b *Bot) ContextSnapshot() common.ContextSnapshot {
	b.mu.Lock()
	defer b.mu.Unlock()

	r := b.ctxMgr.Report()
	r.ToolDefCount = len(b.toolRegistry.Descriptors())
	r.ToolDefTokens = command.EstimateToolDefTokens(b.toolRegistry.Descriptors())

	used := r.SystemPrompt + r.ToolDefTokens + r.TodoText + r.SkillList + r.Messages
	free := r.Budget - used
	if free < 0 {
		free = 0
	}
	percentUsed := 0.0
	if r.Budget > 0 {
		percentUsed = float64(used) / float64(r.Budget)
		if percentUsed > 1 {
			percentUsed = 1
		}
	}
	governance := ""
	if b.ag != nil {
		governance = b.ag.GovernanceLine()
	}

	return common.ContextSnapshot{
		Budget:          r.Budget,
		Used:            used,
		Free:            free,
		PercentUsed:     percentUsed,
		SystemPrompt:    r.SystemPrompt,
		ToolDefTokens:   r.ToolDefTokens,
		TodoText:        r.TodoText,
		SkillList:       r.SkillList,
		MessageTokens:   r.Messages,
		ToolDefCount:    r.ToolDefCount,
		MessageCount:    r.UserMessages + r.AssistantMsgs + r.ToolResults,
		UserMessages:    r.UserMessages,
		AssistantMsgs:   r.AssistantMsgs,
		ToolResults:     r.ToolResults,
		Archived:        r.Archived,
		CompactCount:    r.CompactCount,
		TrimCount:       r.TrimCount,
		CacheHitTokens:  r.CacheHitTokens,
		CacheMissTokens: r.CacheMissTokens,
		CacheHitRatio:   r.CacheHitRatio,
		SubCount:        r.SubCount,
		SubTokens:       r.SubTokens,
		SubCacheHit:     r.SubCacheHit,
		SubCacheMiss:    r.SubCacheMiss,
		Governance:      governance,
		Segments: []common.ContextSegment{
			{Key: "system", Label: "系统提示", Tokens: r.SystemPrompt, Tone: "muted"},
			{Key: "tools", Label: "工具定义", Tokens: r.ToolDefTokens, Tone: "blue"},
			{Key: "todo", Label: "待办", Tokens: r.TodoText, Tone: "orange"},
			{Key: "skills", Label: "Skills", Tokens: r.SkillList, Tone: "yellow"},
			{Key: "messages", Label: "对话消息", Tokens: r.Messages, Tone: "violet"},
			{Key: "free", Label: "剩余", Tokens: free, Tone: "free"},
		},
	}
}

func (b *Bot) SelectSkill(name string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	sk, ok := b.ext.skills.GetForCommand(name)
	if !ok {
		return fmt.Errorf("skill %q not found", name)
	}
	command.ClearSkillContext(b.ctxMgr, b.skillState)
	b.skillState.MsgStart = b.ctxMgr.Len()
	b.ctxMgr.Add("user", sk.Context)
	b.skillState.MsgEnd = b.ctxMgr.Len()
	b.skillState.Hint = name
	b.ext.skills.MarkLoaded(name)
	return nil
}

func (b *Bot) ClearSelectedSkill() {
	b.mu.Lock()
	defer b.mu.Unlock()
	command.ClearSkillContext(b.ctxMgr, b.skillState)
	b.skillState.Hint = ""
	b.skillState.WantsAgent = false
}
