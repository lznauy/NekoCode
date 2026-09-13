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
		subLLM := provider.New(provider.Config{
			APIKey: fm.APIKey, BaseURL: fm.BaseURL, Model: fm.Model, Protocol: fm.Protocol,
			Reasoning: resolvedReasoning(fm),
		})
		subLLM.SetDisableThinking(true)
		engine := subagent.New(subagent.Config{
			LLM: subLLM, Tools: registry, CompactionModel: compactionModel,
		})
		skillContents, err := b.delegatedSkillContents(spec.Skills)
		if err != nil {
			return nil, err
		}
		cfg, ok := buildSubagentRunConfig(ctx, spec, skillContents, contextWindow, autoCompactPercent, ag, b.sess.CurrentID(), b.environment)
		if !ok {
			return nil, fmt.Errorf("unknown sub-agent profile: %s", spec.Profile)
		}
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
	skillContents []string,
	contextWindow, autoCompactPercent int,
	ag *agentcore.Agent,
	sessionID string,
	environment prompt.EnvironmentProvider,
) (subagent.RunConfig, bool) {
	profile, ok := subagent.GetProfile(spec.Profile)
	if !ok {
		return subagent.RunConfig{}, false
	}
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
	if subCB, ok := taskbridge.TaskCallbackFromCtx(ctx); ok {
		cfg.OnToolCall = func(ev subagent.ToolCallEvent) {
			subCB(protocol.StepEvent{
				Action:   ev.Action,
				CallID:   ev.CallID,
				ToolName: ev.ToolName,
				ToolArgs: ev.ToolArgs,
				Output:   ev.Output,
				IsError:  ev.IsError,
			})
		}
	}
	if phaseFn := ag.PhaseFn(); phaseFn != nil {
		cfg.OnPhase = func(p string) { phaseFn(profile.Name + " · " + p) }
	}
	return cfg, true
}

func (b *Bot) delegatedSkillContents(names []string) ([]string, error) {
	seen := make(map[string]struct{}, len(names))
	contents := make([]string, 0, len(names))
	for _, name := range names {
		if name == "" {
			return nil, fmt.Errorf("delegated skill name cannot be empty")
		}
		if _, duplicate := seen[name]; duplicate {
			continue
		}
		command, ok := b.ext.Skill(name)
		if !ok {
			return nil, fmt.Errorf("unknown delegated skill: %s", name)
		}
		seen[name] = struct{}{}
		contents = append(contents, command.Context)
	}
	return contents, nil
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
