package command

import (
	"fmt"
	"strings"

	"nekocode/bot/agent"
	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/extension/skill"
	"nekocode/bot/hooks"
	"nekocode/bot/prompt"
	"nekocode/bot/tools"
	"nekocode/common"
)

// SkillState tracks skill-related state shared between bot and command packages.
type SkillState struct {
	MsgStart   int
	MsgEnd     int
	WantsAgent bool
	Hint       string
}

// Deps bundles services needed by registration and lifecycle operations.
type Deps struct {
	CtxMgr        *ctxmgr.Manager
	Ag            func() *agent.Agent // dynamic: returns current agent
	SkillReg      *skill.Registry
	ToolRegistry  *tools.Registry
	ContextWindow int
	GetConfigFn   func() (provider, model string)           // dynamic config for /config and /model
	ListModelsFn  func() []string                           // available model names for /model
	FreshStart    func() (string, error)                    // /new callback
	SwitchModel   func(name string) (string, string, error) // /model callback
}

// RegisterAll wires built-in and dynamic slash commands.
func RegisterAll(p *Parser, deps Deps, st *SkillState) {
	RegisterDefaults(p, deps)

	// /plan: enter read-only exploration mode.
	p.Register("plan", func(cmd *Command) (string, bool) {
		if len(cmd.Args) == 0 {
			return "Usage: /plan <task>", true
		}
		deps.Ag().SetPlanMode(true)
		deps.CtxMgr.SetSystemPrompt(prompt.PlanModePrompt(strings.Join(cmd.Args, " ")))
		deps.CtxMgr.Add("user", strings.Join(cmd.Args, " "))
		st.WantsAgent = true
		return "", false
	})

	// /skill-name for each loaded skill.
	for _, sk := range deps.SkillReg.List() {
		name := sk.Name
		p.Register(name, func(cmd *Command) (string, bool) {
			sk, ok := deps.SkillReg.Get(name)
			if !ok {
				return fmt.Sprintf("Skill %q not found.", name), true
			}
			st.MsgStart = deps.CtxMgr.Len()
			deps.CtxMgr.Add("user", skill.FormatForContext(sk))
			deps.SkillReg.MarkLoaded(name)
			deps.CtxMgr.SetSkillList(skill.BuildSkillListText(deps.SkillReg.List(), deps.SkillReg.LoadedSet(), deps.ContextWindow))
			if len(cmd.Args) == 0 {
				st.MsgStart = -1
				return fmt.Sprintf("Loaded skill %q.", name), true
			}
			deps.CtxMgr.Add("user", strings.Join(cmd.Args, " "))
			st.MsgEnd = deps.CtxMgr.Len()
			st.Hint = name
			st.WantsAgent = true
			return "", false
		})
	}

	// Skill tool OnLoad callback.
	if t, err := deps.ToolRegistry.Get("skill"); err == nil {
		t.(*skill.SkillTool).SetOnLoad(func(name string) {
			deps.SkillReg.MarkLoaded(name)
			deps.CtxMgr.SetSkillList(skill.BuildSkillListText(deps.SkillReg.List(), deps.SkillReg.LoadedSet(), deps.ContextWindow))
		})
	}
}

// SummarizeIfNeeded compacts context if usage exceeds budget.
func SummarizeIfNeeded(ctxMgr *ctxmgr.Manager) {
	if ctxMgr.NeedsSummarization() {
		_ = ctxMgr.Summarize()
	}
}

// ForceSummarize compacts context now.
func ForceSummarize(ctxMgr *ctxmgr.Manager) (string, error) {
	count, tokens, hasSummary := ctxMgr.Stats()
	if count <= 2 {
		return "Conversation too short, nothing to compact.", nil
	}
	if !ctxMgr.NeedsSummarization() {
		return fmt.Sprintf("Not needed: %d messages, ~%d tokens", count, tokens), nil
	}
	if err := ctxMgr.Summarize(); err != nil {
		return "", err
	}
	_, newTokens, _ := ctxMgr.Stats()
	if newTokens >= tokens {
		return fmt.Sprintf("Already compact: %d messages, ~%d tokens", count, tokens), nil
	}
	action := "Compacted"
	if hasSummary {
		action = "Summary updated"
	}
	return fmt.Sprintf("%s: %d messages, ~%d → ~%d tokens", action, count, tokens, newTokens), nil
}

// ContextStats returns a one-line conversation size summary with a colored bar.
func ContextStats(ctxMgr *ctxmgr.Manager) string {
	r := ctxMgr.Report()
	used := r.SystemPrompt + r.ToolDefTokens + r.TodoText + r.SkillList + r.Messages
	free := r.Budget - used
	if free < 0 {
		free = 0
	}
	bar := ctxmgr.BuildBar(r.Budget, []ctxmgr.BarSegment{
		{Size: r.SystemPrompt, Kind: "sys"},
		{Size: r.ToolDefTokens, Kind: "tools"},
		{Size: r.TodoText, Kind: "todo"},
		{Size: r.SkillList, Kind: "skills"},
		{Size: r.Messages, Kind: "msgs"},
		{Size: free, Kind: "free"},
	}, 20)
	return fmt.Sprintf("%s  %s / %s", bar, common.FormatTokens(used), common.FormatTokens(r.Budget))
}

// ContextReport returns a detailed context window breakdown.
func ContextReport(ctxMgr *ctxmgr.Manager, toolDescs []tools.Descriptor) string {
	r := ctxMgr.Report()
	r.ToolDefCount = len(toolDescs)
	r.ToolDefTokens = estimateToolDefTokens(toolDescs)
	return ctxmgr.FormatContextReport(r)
}

// ForceFreshStart archives current conversation and starts a new session.
func ForceFreshStart(ctxMgr *ctxmgr.Manager, skillReg *skill.Registry, hookReg *hooks.Registry, contextWindow int) (string, error) {
	count, oldTokens, _ := ctxMgr.Stats()
	skillReg.ClearLoaded()
	ctxMgr.SetSkillList(skill.BuildSkillListText(skillReg.List(), nil, contextWindow))
	// Reset hook session state so guards like completionQualityHook
	// don't carry stale flags across /new boundaries.
	if hookReg != nil {
		hookReg.ResetSession()
	}
	if count <= 2 {
		ctxMgr.FreshStart()
		return "New session started.", nil
	}
	if ctxMgr.NeedsSummarization() {
		if err := ctxMgr.Summarize(); err != nil {
			return "", err
		}
	}
	ctxMgr.FreshStart()
	_, newTokens, hasSummary := ctxMgr.Stats()
	d := "no summary"
	if hasSummary {
		d = "with summary"
	}
	return fmt.Sprintf("%d messages, ~%d tokens → %s (~%d tokens)", count, oldTokens, d, newTokens), nil
}

// ClearSkillContext removes skill messages from the previous turn.
func ClearSkillContext(ctxMgr *ctxmgr.Manager, st *SkillState) {
	if st.MsgStart < 0 || st.MsgEnd <= st.MsgStart {
		return
	}
	ctxMgr.RemoveMessages(st.MsgStart, st.MsgEnd-1)
	st.MsgStart = -1
	st.MsgEnd = 0
}

func estimateToolDefTokens(descs []tools.Descriptor) int {
	n := 0
	for _, d := range descs {
		n += len(d.Name) + len(d.Description) + 80
		for _, p := range d.Parameters {
			n += len(p.Name) + len(p.Description) + len(p.Type) + 20
		}
	}
	return n / 4
}
