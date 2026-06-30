package app

import (
	"fmt"

	"nekocode/bot/agent/runtime"
	"nekocode/bot/command"
	"nekocode/bot/config"
	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/common"
)

func (b *Bot) Steer(msg string) { b.getAgent().Steer(msg) }
func (b *Bot) Abort()           { b.getAgent().Abort() }

func (b *Bot) ProviderModel() (string, string) {
	am := b.cfg.ActiveModelConfig()
	return am.Provider, am.Model
}

func (b *Bot) CommandNames() []string { return b.cmdParser.Commands() }

func (b *Bot) ExecuteCommand(input string) (string, common.CmdResult) {
	b.skillState.WantsAgent = false
	cmd := b.cmdParser.Parse(input)
	if cmd.Name == "" {
		command.ClearSkillContext(b.ctxMgr, b.skillState)
		return "", common.CmdNone
	}
	resp, _ := b.cmdParser.Execute(cmd)

	resumed := b.sess.DrainResumed()
	result := commandResult(b.cb.pendingConfirmation(), resumed)
	return resp, result
}

func commandResult(pendingConfirm, sessionResumed bool) common.CmdResult {
	switch {
	case pendingConfirm:
		return common.CmdConfirming
	case sessionResumed:
		return common.CmdSessionResumed
	default:
		return common.CmdHandled
	}
}

func (b *Bot) SkillHint() (string, bool) {
	hint := b.skillState.Hint
	cont := b.skillState.WantsAgent
	b.skillState.Hint = ""
	b.skillState.WantsAgent = false
	return hint, cont
}

func (b *Bot) getAgent() *runtime.Agent {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.ag
}

func (b *Bot) Run(input string, callbacks common.RunCallbacks) (string, error) {
	if callbacks.Text != nil || callbacks.Reason != nil {
		b.SetCallbacks(callbacks.Text, callbacks.Reason)
	}
	return b.RunAgent(input, callbacks.Step)
}

func (b *Bot) RunAgent(input string, onStep func(action, toolName, toolArgs, output string)) (string, error) {
	ag := b.getAgent()
	result := ag.Run(input, onStep)
	ag.SetPlanMode(false)
	b.ctxMgr.SetSystemPrompt(b.promptBuilder.Build())
	command.SummarizeIfNeeded(b.ctxMgr)
	b.sess.Save()
	return result.FinalOutput, result.Error
}

func (b *Bot) ConfigView() config.View {
	b.mu.Lock()
	defer b.mu.Unlock()
	return config.NewView(*b.cfg)
}

func (b *Bot) ApplyConfig(view config.View) (config.View, error) {
	next := view.Config()
	if err := config.Validate(&next); err != nil {
		return config.View{}, err
	}
	if err := config.Save(next); err != nil {
		return config.View{}, err
	}

	b.mu.Lock()
	oldPrompt, oldCompl := 0, 0
	if b.ag != nil {
		oldPrompt, oldCompl = b.ag.TokenUsage()
	}
	b.cfg = &next
	b.mu.Unlock()

	go b.reloadRuntime(oldPrompt, oldCompl)

	return config.NewView(next), nil
}

func (b *Bot) reloadRuntime(oldPrompt, oldCompl int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.reinit()
	if b.ag != nil {
		b.ag.AddTokens(oldPrompt, oldCompl)
	}
}

func (b *Bot) SwitchModel(name string) (string, string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.cfg.SwitchModel(name) {
		return "", "", fmt.Errorf("model %q not found. Available: %v", name, b.cfg.AllModelNames())
	}

	oldPrompt, oldCompl := b.ag.TokenUsage()
	b.initAgent()
	b.ag.AddTokens(oldPrompt, oldCompl)
	b.ctxMgr.ResetCache()

	am := b.cfg.ActiveModelConfig()
	return am.Model, am.Provider, nil
}

func (b *Bot) ContextStatus() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return command.ContextStats(b.ctxMgr)
}

func (b *Bot) ContextReport() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return command.ContextReport(b.ctxMgr, b.toolRegistry.Descriptors())
}

func (b *Bot) ContextSnapshot() common.ContextSnapshot {
	b.mu.Lock()
	defer b.mu.Unlock()

	r := b.ctxMgr.Report()
	r.ToolDefCount = len(b.toolRegistry.Descriptors())
	r.ToolDefTokens = command.EstimateToolDefTokens(b.toolRegistry.Descriptors())
	return buildContextSnapshot(r)
}

func buildContextSnapshot(r ctxmgr.ContextReport) common.ContextSnapshot {
	used := r.SystemPrompt + r.ToolDefTokens + r.TodoText + r.SkillList + r.Messages
	free := max(r.Budget-used, 0)
	percentUsed := 0.0
	if r.Budget > 0 {
		percentUsed = min(float64(used)/float64(r.Budget), 1)
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

func (b *Bot) SelectSkill(name string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	skills := skillCommandProvider{manager: b.ext.skills}
	sk, ok := skills.GetForCommand(name)
	if !ok {
		return fmt.Errorf("skill %q not found", name)
	}
	command.ClearSkillContext(b.ctxMgr, b.skillState)
	b.skillState.MsgStart = b.ctxMgr.Len()
	b.ctxMgr.Add("user", sk.Context)
	b.skillState.MsgEnd = b.ctxMgr.Len()
	b.skillState.Hint = name
	skills.MarkLoaded(name)
	return nil
}

func (b *Bot) ClearSelectedSkill() {
	b.mu.Lock()
	defer b.mu.Unlock()
	command.ClearSkillContext(b.ctxMgr, b.skillState)
	b.skillState.Hint = ""
	b.skillState.WantsAgent = false
}

func (b *Bot) SkillManagementView() common.SkillManagementView {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.ext.SkillManagementView()
}

func (b *Bot) SetPluginEnabled(name string, enabled bool) (common.SkillManagementView, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.ext.SetPluginEnabled(name, enabled)
}

func (b *Bot) RefreshSkillManagement() common.SkillManagementView {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.ext.RefreshSkillManagement()
}

func (b *Bot) Stats() common.BotStats {
	ag := b.getAgent()

	p, c := ag.TokenUsage()
	tp, tc := ag.TurnTokenUsage()
	d := ag.Duration()
	compactCount, _ := b.ctxMgr.CompactStats()
	return common.BotStats{
		PromptTokens:     p,
		CompletionTokens: c,
		TurnPrompt:       tp,
		TurnCompletion:   tc,
		ContextTokens:    ag.ContextTokens(),
		CompactCount:     compactCount,
		Duration:         common.FormatDuration(d),
	}
}
