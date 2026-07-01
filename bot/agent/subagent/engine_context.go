package subagent

import (
	"strings"

	ctxmgr "nekocode/bot/contextmgr"
	ctxfmt "nekocode/bot/contextmgr/context"
)

func (e *Engine) newContextManager(cfg RunConfig) *ctxmgr.Manager {
	return ctxmgr.NewSub(buildSystemPrompt(cfg), cfg.ContextWindow, e.mergeClient)
}

func buildSystemPrompt(cfg RunConfig) string {
	parts := []string{cfg.AgentType.SystemPrompt}
	if cfg.AgentType.Name == "researcher" && cfg.Thoroughness == thoroughDeep {
		parts[0] = strings.Replace(parts[0],
			"Focus on the specific question. For \"very thorough\": search across multiple directories and naming conventions.",
			"Search across ALL packages, naming conventions, and locations. Read at least 5 files. Be exhaustive.", 1)
	}
	if cfg.Cwd != "" {
		parts = append(parts, ctxfmt.FormatCwd(cfg.Cwd))
	}
	if cfg.ProjectContext != "" && !cfg.AgentType.OmitProjectContext {
		parts = append(parts, cfg.ProjectContext)
	}
	if cfg.Handoff != "" {
		parts = append(parts, "<handoff>\n"+cfg.Handoff+"\n</handoff>")
	}
	return strings.Join(parts, "\n\n")
}

func phaseReporter(cfg RunConfig) func(string) {
	return func(p string) {
		if cfg.OnPhase != nil {
			cfg.OnPhase(p)
		}
	}
}
