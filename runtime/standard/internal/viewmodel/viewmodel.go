package viewmodel

import (
	"strings"

	"nekocode/bot/config"
	"nekocode/bot/contextmgr"
	"nekocode/bot/session"
	controlruntime "nekocode/runtime"
)

func Model(model config.ModelConfig) controlruntime.ModelSelection {
	return controlruntime.ModelSelection{
		Provider: model.Provider, Model: model.Model, ReasoningEffort: model.ReasoningEffort,
	}
}

func Config(cfg config.Config) controlruntime.ConfigView {
	return controlruntime.ConfigView{
		Path:               config.Path(),
		Exists:             config.Exists(),
		Active:             cfg.Active,
		AutoCompactPercent: cfg.EffectiveAutoCompactPercent(),
		FlashModel:         cfg.FlashModel,
		Models:             modelConfigsToView(cfg.Models),
		ImageGenModels:     imageGenConfigsToView(cfg.ImageGenModels),
		MCPServers:         mcpServerConfigsToView(cfg.MCPServers),
		Permissions:        permissionsConfigToView(cfg.Permissions),
		Workspaces:         workspaceConfigsToView(cfg.Workspaces),
		Jev:                jevConfigToView(cfg.Jev),
	}
}

func ToConfig(view controlruntime.ConfigView) config.Config {
	return config.Config{
		Active:             view.Active,
		FlashModel:         view.FlashModel,
		AutoCompactPercent: view.AutoCompactPercent,
		Models:             modelConfigsFromView(view.Models),
		ImageGenModels:     imageGenConfigsFromView(view.ImageGenModels),
		MCPServers:         mcpServerConfigsFromView(view.MCPServers),
		Permissions:        permissionsConfigFromView(view.Permissions),
		Workspaces:         workspaceConfigsFromView(view.Workspaces),
		Jev:                jevConfigFromView(view.Jev),
	}
}

func SessionMetas(list []session.Meta) []controlruntime.SessionMeta {
	out := make([]controlruntime.SessionMeta, 0, len(list))
	for _, meta := range list {
		out = append(out, SessionMeta(meta))
	}
	return out
}

func SessionMeta(meta session.Meta) controlruntime.SessionMeta {
	return controlruntime.SessionMeta{
		ID: meta.ID, CWD: meta.CWD, CreatedAt: meta.CreatedAt,
		UpdatedAt: meta.UpdatedAt, MsgCount: meta.MsgCount,
	}
}

func SessionSnapshot(snapshot *session.Snapshot) controlruntime.SessionMeta {
	if snapshot == nil {
		return controlruntime.SessionMeta{}
	}
	return controlruntime.SessionMeta{
		ID: snapshot.ID, CWD: snapshot.CWD,
		CreatedAt: snapshot.CreatedAt, UpdatedAt: snapshot.UpdatedAt,
		MsgCount: len(snapshot.Messages),
	}
}

func Memory(scope controlruntime.MemoryScope, path, content string) controlruntime.MemoryView {
	content = strings.TrimSpace(content)
	if scope == "" {
		scope = controlruntime.MemoryScopeProject
	}
	return controlruntime.MemoryView{
		Scope:    scope,
		Path:     path,
		Content:  content,
		Sections: memorySections(content),
		Empty:    content == "",
	}
}

func memorySections(content string) []controlruntime.MemorySection {
	sections := []controlruntime.MemorySection{
		{Key: "tech_stack", Title: "## Tech Stack", Empty: true},
		{Key: "active_goals", Title: "## Active Goals", Empty: true},
		{Key: "completed_tasks", Title: "## Completed Tasks", Empty: true},
		{Key: "architecture_map", Title: "## Key Architecture Map", Empty: true},
		{Key: "preferences", Title: "## User Preferences", Empty: true},
	}
	byTitle := make(map[string]int, len(sections))
	for i, section := range sections {
		byTitle[section.Title] = i
	}

	current := -1
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || trimmed == "[Project Memory]" {
			continue
		}
		if index, ok := byTitle[trimmed]; ok {
			current = index
			continue
		}
		if current < 0 {
			continue
		}
		if sections[current].Content != "" {
			sections[current].Content += "\n"
		}
		sections[current].Content += line
	}
	for i := range sections {
		sections[i].Content = strings.TrimSpace(sections[i].Content)
		sections[i].Empty = sections[i].Content == ""
	}
	return sections
}

func ContextSnapshot(report contextmgr.ContextReport) controlruntime.ContextSnapshot {
	used := report.SystemPrompt + report.ToolDefTokens + report.TodoText + report.SkillList + report.Memory + report.Archive + report.Messages
	free := max(report.Budget-used, 0)
	percentUsed := 0.0
	if report.Budget > 0 {
		percentUsed = min(float64(used)/float64(report.Budget), 1)
	}

	return controlruntime.ContextSnapshot{
		Budget: report.Budget, Used: used, Free: free, PercentUsed: percentUsed,
		SystemPrompt: report.SystemPrompt, ToolDefTokens: report.ToolDefTokens,
		TodoText: report.TodoText, SkillList: report.SkillList, MessageTokens: report.Messages,
		ToolDefCount: report.ToolDefCount,
		MessageCount: report.UserMessages + report.AssistantMsgs + report.ToolResults,
		UserMessages: report.UserMessages, AssistantMsgs: report.AssistantMsgs,
		ToolResults: report.ToolResults, Archived: report.Archived,
		CompactCount: report.CompactCount, CompactionThreshold: report.CompactionThreshold,
		TrimCount:      report.TrimCount,
		CacheHitTokens: report.CacheHitTokens, CacheMissTokens: report.CacheMissTokens,
		CacheHitRatio: report.CacheHitRatio, SubCount: report.SubCount,
		SubTokens: report.SubTokens, SubCacheHit: report.SubCacheHit,
		SubCacheMiss: report.SubCacheMiss,
		Segments: []controlruntime.ContextSegment{
			{Key: "system", Label: "系统提示", Tokens: report.SystemPrompt, Tone: "muted"},
			{Key: "tools", Label: "工具定义", Tokens: report.ToolDefTokens, Tone: "blue"},
			{Key: "todo", Label: "待办", Tokens: report.TodoText, Tone: "orange"},
			{Key: "skills", Label: "Skills", Tokens: report.SkillList, Tone: "yellow"},
			{Key: "memory", Label: "Memory", Tokens: report.Memory, Tone: "muted"},
			{Key: "archive", Label: "归档摘要", Tokens: report.Archive, Tone: "muted"},
			{Key: "messages", Label: "对话消息", Tokens: report.Messages, Tone: "violet"},
			{Key: "free", Label: "剩余", Tokens: free, Tone: "free"},
		},
	}
}
