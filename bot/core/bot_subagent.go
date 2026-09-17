package core

import (
	"context"
	"fmt"

	agentcore "nekocode/bot/agent"
	"nekocode/bot/agent/subagent"
	"nekocode/bot/config"
	"nekocode/bot/extension/tool/runtime/taskbridge"
	"nekocode/bot/prompt"
	"nekocode/bot/provider"
	"nekocode/protocol"
)

func (b *Bot) wireTaskTool(fm config.ModelConfig, compactionModel provider.LLM, ag *agentcore.Agent) {
	registry := b.toolbox.Registry
	ctxMgr := b.ctxMgr
	contextWindow := b.cfg.EffectiveContextWindow()
	autoCompactPercent := b.cfg.EffectiveAutoCompactPercent()

	b.toolbox.WireTaskRunner(func(ctx context.Context, spec taskbridge.TaskSpec) (*taskbridge.TaskResult, error) {
		profile, err := b.ext.AgentProfile(spec.Profile)
		if err != nil {
			return nil, err
		}
		subLLM := provider.New(provider.Config{
			APIKey: fm.APIKey, BaseURL: fm.BaseURL, Model: fm.Model, Protocol: fm.Protocol,
			Reasoning: resolvedReasoning(fm),
		})
		subLLM.SetDisableThinking(true)
		engine := subagent.New(subagent.Config{
			LLM: subLLM, Tools: registry, CompactionModel: compactionModel,
		})
		skillNames, skillContents, err := b.delegatedSkills(append(profile.Skills, spec.Skills...))
		if err != nil {
			return nil, err
		}
		if cb, ok := taskbridge.TaskCallbackFromCtx(ctx); ok {
			cb(protocol.StepEvent{
				Action:       protocol.StepActionSubAgentStart,
				SubAgentType: profile.Name, SubAgentProfile: profile.Name,
				SubAgentSkills: skillNames,
			})
		}
		cfg := buildSubagentRunConfig(ctx, spec, profile, skillContents, contextWindow, autoCompactPercent, ag, b.sess.CurrentID(), b.environment)
		result, err := engine.Run(ctx, cfg)
		if result != nil && (result.CacheHitTokens > 0 || result.CacheMissTokens > 0) {
			ctxMgr.RecordSubagent(result.TotalTokens, result.CacheHitTokens, result.CacheMissTokens)
		}
		return subagentTaskResult(result), err
	})
}

func buildSubagentRunConfig(
	ctx context.Context,
	spec taskbridge.TaskSpec,
	profile subagent.Profile,
	skillContents []string,
	contextWindow, autoCompactPercent int,
	ag *agentcore.Agent,
	sessionID string,
	environment prompt.EnvironmentProvider,
) subagent.RunConfig {
	cfg := subagent.RunConfig{
		Prompt:             spec.Prompt,
		Profile:            profile,
		SkillContents:      skillContents,
		ContextWindow:      contextWindow,
		AutoCompactPercent: autoCompactPercent,
		ConfirmFn:          ag.ConfirmFn(),
		FullAccess:         ag.Executor().FullAccess,
		ToolState:          ag.ToolExecutionState(),
		AddTokens: func(_ int, completion int) {
			ag.AddCompletionTokens(completion)
		},
		RecordLLMUsage: ag.RecordLLMUsage,
		SessionID:      sessionID,
		Policy:         ag.Governance(),
		Environment:    environment,
	}
	if confirm := cfg.ConfirmFn; confirm != nil {
		cfg.ConfirmFn = func(request protocol.ConfirmRequest) protocol.ConfirmReply {
			request.SubAgentID = taskbridge.TaskIDFromCtx(ctx)
			return confirm(request)
		}
	}
	if subCB, ok := taskbridge.TaskCallbackFromCtx(ctx); ok {
		cfg.OnText = func(text string) { subCB(protocol.StepEvent{Action: protocol.StepActionSubAgentText, Output: text}) }
		cfg.OnReasoning = func(text string) { subCB(protocol.StepEvent{Action: protocol.StepActionSubAgentReason, Output: text}) }
		cfg.OnMessage = func(text string) { subCB(protocol.StepEvent{Action: protocol.StepActionSubAgentMessage, Output: text}) }

		cfg.OnToolCall = func(ev subagent.ToolCallEvent) {
			subCB(protocol.StepEvent{
				Action:   ev.Action,
				CallID:   ev.CallID,
				ToolName: ev.ToolName,
				ToolArgs: ev.ToolArgs, ToolInput: ev.ToolInput,
				Output:  ev.Output,
				IsError: ev.IsError,
			})
		}
	}
	if phaseFn := ag.PhaseFn(); phaseFn != nil {
		cfg.OnPhase = func(p string) { phaseFn(profile.Name + " · " + p) }
	}
	return cfg
}

func (b *Bot) delegatedSkills(names []string) ([]string, []string, error) {
	seen := make(map[string]struct{}, len(names))
	var resolved []string
	contents := make([]string, 0, len(names))
	for _, name := range names {
		if name == "" {
			return nil, nil, fmt.Errorf("delegated skill name cannot be empty")
		}
		if _, duplicate := seen[name]; duplicate {
			continue
		}
		command, ok := b.ext.Skill(name)
		if !ok {
			return nil, nil, fmt.Errorf("unknown delegated skill: %s", name)
		}
		seen[name] = struct{}{}
		resolved = append(resolved, name)
		contents = append(contents, command.Context)
	}
	return resolved, contents, nil
}

func subagentTaskResult(result *subagent.Result) *taskbridge.TaskResult {
	if result == nil {
		return nil
	}
	status := taskbridge.TaskStatusCompleted
	switch result.Status {
	case subagent.StatusFailed:
		status = taskbridge.TaskStatusFailed
	case subagent.StatusPartial:
		status = taskbridge.TaskStatusPartial
	}
	return &taskbridge.TaskResult{
		Status:  status,
		Content: subagent.FormatResult(result),
	}
}
