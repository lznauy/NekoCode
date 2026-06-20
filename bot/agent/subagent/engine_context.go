package subagent

import (
	"strings"

	ctxmgr "nekocode/bot/contextmgr"
	ctxfmt "nekocode/bot/contextmgr/context"
)

func (e *Engine) newContextManager(cfg RunConfig) *ctxmgr.Manager {
	ctxMgr := ctxmgr.NewSub(buildSystemPrompt(cfg), cfg.ContextWindow, e.mergeClient)
	if cfg.Cwd != "" {
		ctxMgr.Add("system", ctxfmt.FormatCwd(cfg.Cwd))
	}
	if cfg.ProjectContext != "" && !cfg.AgentType.OmitProjectContext {
		ctxMgr.Add("system", cfg.ProjectContext)
	}
	return ctxMgr
}

func buildSystemPrompt(cfg RunConfig) string {
	systemPrompt := cfg.AgentType.SystemPrompt
	if cfg.AgentType.Name == "researcher" && cfg.Thoroughness == thoroughDeep {
		systemPrompt = strings.Replace(systemPrompt,
			"Focus on the specific question. For \"very thorough\": search across multiple directories and naming conventions.",
			"Search across ALL packages, naming conventions, and locations. Read at least 5 files. Be exhaustive.", 1)
	}
	if cfg.Handoff != "" {
		systemPrompt += "\n\n<handoff>\n" + cfg.Handoff + "\n</handoff>"
	}
	return systemPrompt
}

func phaseReporter(cfg RunConfig) func(string) {
	return func(p string) {
		if cfg.OnPhase != nil {
			cfg.OnPhase(p)
		}
	}
}

func (e *Engine) applyThinkingMode(cfg RunConfig) func() {
	if !cfg.DisableThinking {
		return func() {}
	}
	prev := e.llmClient.GetDisableThinking()
	e.llmClient.SetDisableThinking(true)
	return func() { e.llmClient.SetDisableThinking(prev) }
}
