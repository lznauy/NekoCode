package app

import (
	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/common"
)

func buildContextSnapshot(r ctxmgr.ContextReport) common.ContextSnapshot {
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
