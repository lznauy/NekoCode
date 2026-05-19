package bot

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"nekocode/bot/agent"
	"nekocode/bot/agent/subagent"
	"nekocode/bot/command"
	"nekocode/bot/config"
	"nekocode/bot/ctxmgr"
	"nekocode/bot/ctxmgr/compact"
	"nekocode/bot/ctxmgr/memory"
	"nekocode/bot/hooks"
	"nekocode/bot/projctx"
	"nekocode/bot/prompt"
	"nekocode/bot/session"
	"nekocode/bot/skill"
	"nekocode/bot/skill/bundled"
	"nekocode/bot/tools"
	"nekocode/bot/tools/builtin"
	"nekocode/llm"
)

type Bot struct {
	cfg           *config.Config
	ctxMgr        *ctxmgr.Manager
	cmdParser     *command.Parser
	skillState    *command.SkillState
	ag            *agent.Agent
	sess          *session.Snapshot
	skillReg      *skill.Registry
	promptBuilder *prompt.Builder
	toolRegistry  *tools.Registry
}

func New() *Bot {
	b := &Bot{}
	ctx := context.Background()

	b.initConfig()
	projCtx, projIndex := b.initCtxMgr()
	llmClient := b.initLLM(ctx)
	b.initSummarizer(ctx, llmClient)
	b.initToolRegistry(projIndex)
	b.initSkills()
	b.initSession()
	b.initAgent(llmClient, projCtx)
	b.initGuardrails()
	b.initCommands()

	return b
}

// -- init blocks ----------------------------------------------------------

func (b *Bot) initConfig() {
	b.cfg, _ = config.Load()
	cwd, _ := os.Getwd()
	b.promptBuilder = prompt.NewBuilder(cwd)
}

func (b *Bot) initCtxMgr() (projCtx string, projIndex *projctx.ProjectIndex) {
	systemPrompt := b.promptBuilder.Build()
	memFile, _ := memory.Load(memory.DefaultPath())
	b.ctxMgr = ctxmgr.New(systemPrompt, memFile)

	cwd, err := os.Getwd()
	if err != nil {
		b.ctxMgr.SetTokenBudget(b.cfg.TokenBudget)
		return "", nil
	}

	projCtx = projctx.LoadProjectContext(cwd)
	if projCtx != "" {
		b.ctxMgr.Add("system", projCtx)
	}

	if idx, err := projctx.IndexProject(cwd); err == nil {
		if skeleton := idx.FormatSkeleton(); skeleton != "" {
			b.ctxMgr.Add("system", skeleton)
		}
		projIndex = idx
	}

	b.ctxMgr.SetTokenBudget(b.cfg.TokenBudget)
	return projCtx, projIndex
}

func (b *Bot) initLLM(_ context.Context) llm.LLM {
	llmClient := llm.NewClient(b.cfg.Provider, b.cfg.APIKey, b.cfg.BaseURL, b.cfg.Model, b.cfg.ThinkingBudget)

	mergeClient := llm.Clone(b.cfg.Provider, b.cfg.APIKey, b.cfg.BaseURL, b.cfg.Model, b.cfg.ThinkingBudget)
	mergeClient.SetDisableThinking(true)
	mergeClient.SetMaxTokens(2000)
	b.ctxMgr.SetMergeClient(mergeClient)

	return llmClient
}

func (b *Bot) initSummarizer(ctx context.Context, llmClient llm.LLM) {
	b.ctxMgr.SetSummarizer(func(msgs []llm.Message, prevSummary string) (string, error) {
		prompt := compact.BuildPrompt(msgs, prevSummary)
		resp, err := llmClient.Chat(ctx, []llm.Message{{Role: "user", Content: prompt}}, nil)
		if err != nil {
			return "", err
		}
		if len(resp.Choices) > 0 {
			return resp.Choices[0].Message.Content, nil
		}
		return "", nil
	})
}

func (b *Bot) initToolRegistry(projIndex *projctx.ProjectIndex) {
	b.toolRegistry = tools.NewRegistry()
	builtin.RegisterAll(b.toolRegistry)

	if projIndex != nil {
		builtin.RegisterProjectInfo(b.toolRegistry, projIndex)
	}

	tools.GlobalFileCache = tools.NewFileStateCache()
}

func (b *Bot) initSkills() {
	b.skillReg = skill.NewRegistry()
	b.skillReg.RegisterBundled(bundled.All())
	if err := b.skillReg.Load(skill.DefaultDirs()); err != nil {
		fmt.Fprintf(os.Stderr, "skill: load error: %v\n", err)
	}
	b.toolRegistry.Register(skill.NewSkillTool(b.skillReg))
	b.ctxMgr.SetSkillList(skill.BuildSkillListText(b.skillReg.List(), nil, b.cfg.TokenBudget))
}

func (b *Bot) initSession() {
	b.cmdParser = command.NewParser()
	b.skillState = &command.SkillState{MsgStart: -1}

	cwd, _ := os.Getwd()
	sess, err := session.New(cwd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "session: %v — running without session persistence\n", err)
		return
	}
	b.sess = sess
}

func (b *Bot) initAgent(llmClient llm.LLM, projCtx string) {
	b.ag = agent.New(context.Background(), b.ctxMgr, llmClient, b.toolRegistry)
	hr := hooks.NewRegistry()
	hooks.RegisterBuiltin(hr)
	b.ag.SetHookRegistry(hr)

	subLLM := llm.Clone(b.cfg.Provider, b.cfg.APIKey, b.cfg.BaseURL, b.cfg.Model, b.cfg.ThinkingBudget)
	engine := subagent.NewEngine(subLLM, b.toolRegistry)

	types := make(map[string]subagent.AgentType)
	names := make([]string, 0)
	for _, a := range subagent.List() {
		names = append(names, a.Name)
		types[a.Name] = a
	}

	if t, err := b.toolRegistry.Get("task"); err == nil {
		cwd, _ := os.Getwd()
		t.(*builtin.TaskTool).Wire(func(ctx context.Context, prompt, agentType, thoroughness string) (*subagent.Result, error) {
			at, ok := subagent.Get(agentType)
			if !ok {
				return nil, fmt.Errorf("unknown sub-agent type: %s (available: %s)", agentType, strings.Join(names, ", "))
			}
			cfg := subagent.RunConfig{
				Prompt:          prompt,
				AgentType:       at,
				Cwd:             cwd,
				ProjectContext:  projCtx,
				Thoroughness:    thoroughness,
					TokenBudget:     b.cfg.TokenBudget,
				DisableThinking: true,
			}
			if fn := b.ag.PhaseFn(); fn != nil {
				cfg.OnPhase = func(p string) { fn(at.Name + " · " + p) }
			}
			cfg.AddTokens = b.ag.AddTokens
			return engine.Run(ctx, cfg)
		}, types)
	}
}

func (b *Bot) initGuardrails() {
	b.ag.SetContextTransform(func(msgs []llm.Message) []llm.Message {
		toolResults := 0
		for _, m := range msgs {
			if m.Role == "tool" {
				toolResults++
			}
		}
		if toolResults > 40 {
			msgs = append(msgs, llm.Message{
				Role:    "user",
				Content: "[System] " + strconv.Itoa(toolResults) + " tool results accumulated. Check for unfinished sub-tasks — if any, continue with task. If all done, call task(verify) to validate, then report results.",
			})
		}
		return msgs
	})
}

func (b *Bot) initCommands() {
	command.RegisterAll(b.cmdParser, command.Deps{
		CtxMgr:        b.ctxMgr,
		Ag:            b.ag,
		SkillReg:      b.skillReg,
		ToolRegistry:  b.toolRegistry,
		TokenBudget:   b.cfg.TokenBudget,
		Provider:      b.cfg.Provider,
		Model:         b.cfg.Model,
		PromptBuilder: b.promptBuilder,
		FreshStart: func() (string, error) {
			return command.ForceFreshStart(b.ctxMgr, b.skillReg, b.cfg.TokenBudget)
		},
	}, b.skillState)

	b.cmdParser.Register("sessions", func(cmd *command.Command) (string, bool) {
		if len(cmd.Args) > 0 {
			id := cmd.Args[0]
			if err := b.ResumeSession(id); err != nil {
				return fmt.Sprintf("Failed to resume session %s: %v", id, err), true
			}
			return fmt.Sprintf("Resumed session %s (%d messages restored).", id, len(b.sess.Messages)), true
		}
		sessions := session.List()
		if len(sessions) == 0 {
			return "No saved sessions.", true
		}
		var sb strings.Builder
		sb.WriteString("Saved sessions:\n")
		for _, s := range sessions {
			sb.WriteString(fmt.Sprintf("  %s  %s  %d msgs  %s\n", s.ID, s.Age(), s.MsgCount, s.CWD))
		}
		sb.WriteString("\n/sessions <id> to resume")
		return sb.String(), true
	})
}

// -- session persistence --------------------------------------------------

func (b *Bot) saveSession() {
	if b.sess == nil {
		return
	}
	sysPrompt, skills, archive, mem, cb, msgs, budget := b.ctxMgr.Snapshot()
	b.sess.SystemPrompt = sysPrompt
	b.sess.Skills = skills
	b.sess.Memory = mem
	b.sess.Archive = archive
	b.sess.Messages = msgs
	b.sess.CompactBoundary = cb
	b.sess.TokenBudget = budget
	b.sess.PromptTokens, b.sess.CompletionTokens = b.ag.TokenUsage()
	for name := range b.skillReg.LoadedSet() {
		b.sess.LoadedSkills = append(b.sess.LoadedSkills, name)
	}
	if err := b.sess.Save(); err != nil {
		fmt.Fprintf(os.Stderr, "session: save error: %v\n", err)
	}
}

func (b *Bot) ResumeSession(id string) error {
	sess, err := session.Load(id)
	if err != nil {
		return fmt.Errorf("session: load: %w", err)
	}
	b.ctxMgr.Restore(sess.SystemPrompt, sess.Skills, sess.Archive, sess.Memory,
		sess.CompactBoundary, sess.Messages, sess.TokenBudget)
	b.ag.AddTokens(sess.PromptTokens, sess.CompletionTokens)
	for _, name := range sess.LoadedSkills {
		b.skillReg.MarkLoaded(name)
	}
	b.sess = sess
	return nil
}
