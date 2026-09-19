package command

import (
	"context"
	"fmt"
	"strings"

	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/protocol"
)

// Handler owns command registration, execution, and selected-skill state.
type Handler struct {
	parser *Parser
	skill  *skillState
	ctxMgr *ctxmgr.Manager
}

// SkillRegistration is the command-facing representation of an extension
// skill. The extension module owns discovery and loaded-state updates.
type SkillRegistration struct {
	Name       string
	Load       func() (string, bool)
	MarkLoaded func()
}

// New creates a command handler and registers built-in and dynamic commands.
func New(deps Deps) *Handler {
	h := &Handler{parser: NewParser(), skill: &skillState{MsgStart: -1}, ctxMgr: deps.CtxMgr}
	h.RegisterAll(deps)
	return h
}

// RegisterAll (re)registers built-in commands. Dynamic skills are installed
// separately by RegisterSkills, and their state is preserved.
func (h *Handler) RegisterAll(deps Deps) {
	h.ctxMgr = deps.CtxMgr
	registerAll(h.parser, deps, h.skill)
}

// RegisterSkills replaces all dynamic dollar commands with the extension's
// current skill set.
func (h *Handler) RegisterSkills(skills []SkillRegistration) {
	next := NewParser()
	for _, registration := range skills {
		sk := registration
		next.RegisterDynamicInfo(sk.Name, "Load this skill", func(_ context.Context, cmd *Command) (string, bool) {
			if h.ctxMgr == nil {
				return "Skill context is unavailable.", true
			}
			if sk.Load == nil {
				return fmt.Sprintf("Skill %q not found.", sk.Name), true
			}
			content, ok := sk.Load()
			if !ok {
				return fmt.Sprintf("Skill %q not found.", sk.Name), true
			}
			h.skill.MsgStart = h.ctxMgr.Len()
			h.ctxMgr.Add("user", content)
			if sk.MarkLoaded != nil {
				sk.MarkLoaded()
			}
			if len(cmd.Args) == 0 {
				h.skill.MsgStart = -1
				return fmt.Sprintf("Loaded skill %q.", sk.Name), true
			}
			h.ctxMgr.Add("user", strings.Join(cmd.Args, " "))
			h.skill.MsgEnd = h.ctxMgr.Len()
			h.skill.WantsAgent = true
			return "", false
		})
	}
	h.parser.replacePrefix(DollarPrefix, next.handlers)
}

// Parser exposes the command registry used by extension and session commands.
func (h *Handler) Parser() *Parser {
	return h.parser
}

// Names returns the sorted names of all registered commands.
func (h *Handler) Names() []string {
	return h.parser.Commands()
}

func (h *Handler) Menu(ctx context.Context, input string) (protocol.CommandMenu, bool) {
	return h.parser.Menu(ctx, input)
}

// CommandAvailability reports whether input names a registered command and
// whether that command may execute while a task is in progress.
func (h *Handler) CommandAvailability(input string) (isCommand, duringTask bool) {
	return h.parser.CommandAvailability(input)
}

// Execute parses input and runs it as a command. handled is false when
// input is not a command — in that case any previous skill context is
// cleared from ctxMgr.
func (h *Handler) Execute(ctx context.Context, input string, ctxMgr *ctxmgr.Manager) (string, bool) {
	h.skill.WantsAgent = false
	cmd := h.parser.Parse(input)
	if cmd.Name == "" {
		clearSkillContext(ctxMgr, h.skill)
		return "", false
	}
	return h.parser.Execute(ctx, cmd)
}

// TakeContinuation returns and clears the command's optional agent
// continuation metadata.
func (h *Handler) TakeContinuation() (continueAgent bool) {
	continueAgent = h.skill.WantsAgent
	h.skill.WantsAgent = false
	return continueAgent
}

// SelectSkill loads a skill context into the conversation and records its
// message range so it can be removed later.
func (h *Handler) SelectSkill(ctxMgr *ctxmgr.Manager, context string) {
	clearSkillContext(ctxMgr, h.skill)
	h.skill.MsgStart = ctxMgr.Len()
	ctxMgr.Add("user", context)
	h.skill.MsgEnd = ctxMgr.Len()
}

// ClearSkill removes the selected skill's messages and resets its state.
func (h *Handler) ClearSkill(ctxMgr *ctxmgr.Manager) {
	clearSkillContext(ctxMgr, h.skill)
	h.skill.WantsAgent = false
}

// ResetSkill forgets selected-skill bookkeeping after a session context has
// been replaced.
func (h *Handler) ResetSkill() {
	h.skill.MsgStart = -1
	h.skill.MsgEnd = 0
	h.skill.WantsAgent = false
}
