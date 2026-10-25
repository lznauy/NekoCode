package core

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/google/uuid"
	agentcore "nekocode/bot/agent"
	"nekocode/bot/agent/subagent"
	"nekocode/bot/config"
	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/extension/tool/runtime/taskbridge"
	"nekocode/bot/prompt"
	"nekocode/bot/provider"
	"nekocode/bot/session"
	"nekocode/protocol"
)

func (b *Bot) wireTaskTool(fm config.ModelConfig, compactionModel provider.LLM, ag *agentcore.Agent) {
	registry := b.toolbox.Registry
	ctxMgr := b.ctxMgr
	contextWindow := b.cfg.EffectiveContextWindow()
	autoCompactPercent := b.cfg.EffectiveAutoCompactPercent()

	b.toolbox.WireTaskRunner(func(ctx context.Context, spec taskbridge.TaskSpec) (*taskbridge.TaskResult, error) {
		id := taskbridge.TaskIDFromCtx(ctx)
		savedProfile := ""
		if spec.ResumeID != "" {
			id = spec.ResumeID
			// Resolve inherited capabilities before opening can repair history.
			metadata, err := session.ReadSubagentMetadata(b.sess.CurrentID(), id)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					return nil, fmt.Errorf("cannot resume subagent %q: no saved task with this ID in the current parent session; use task_list items[].id, not latest_attempt.id (an execution attempt ID): %w", id, err)
				}
				return nil, err
			}
			savedProfile = metadata.Profile
			if spec.Profile == "" {
				spec.Profile = savedProfile
			}
		}
		profile, err := b.ext.AgentProfile(spec.Profile)
		if err != nil {
			return nil, err
		}
		if spec.ResumeID != "" && savedProfile != profile.Name {
			return nil, fmt.Errorf("cannot change subagent profile on resume")
		}
		if id == "" {
			id = uuid.NewString()
		}
		// Establish identity before permissions, callbacks and output namespaces.
		ctx = taskbridge.WithTaskID(ctx, id)
		child, err := session.OpenSubagent(b.sess.CurrentID(), id, b.cwd, spec.ResumeID != "")
		if err != nil {
			return nil, fmt.Errorf("open subagent session: %w", err)
		}
		defer child.Close()
		var restored *ctxmgr.ManagerSnapshot
		if spec.ResumeID != "" {
			snapshot := child.Snapshot()
			if spec.Skills == nil {
				spec.Skills = snapshot.Subagent.Skills
			}
			contextSnapshot := snapshot.ContextSnapshot()
			restored = &contextSnapshot
		}
		reasoning := resolvedReasoning(fm)
		reasoning.Disabled = true
		subLLM := provider.New(provider.Config{
			APIKey: fm.APIKey, BaseURL: fm.BaseURL, Model: fm.Model, Protocol: fm.Protocol,
			Reasoning: reasoning,
		})
		engine := subagent.New(subagent.Config{
			LLM: subLLM, Tools: registry, CompactionModel: compactionModel,
		})
		skillNames, skillContents, err := b.delegatedSkills(append(profile.Skills, spec.Skills...))
		if err != nil {
			return nil, err
		}
		if err := child.Begin(profile.Name, spec.Prompt, skillNames, spec.Title); err != nil {
			return nil, err
		}
		attempt := child.Snapshot().Subagent.CurrentAttempt()
		if attempt == nil {
			return nil, fmt.Errorf("subagent attempt was not initialized")
		}
		cfg := buildSubagentRunConfig(ctx, spec, profile, skillContents, contextWindow, autoCompactPercent, ag, b.sess.CurrentID(), b.environment, attempt.ID, attempt.Seq)
		b.mu.Lock()
		if b.promptBuilder != nil {
			cfg.ProjectInstructions = b.promptBuilder.BuildProjectInstructions()
		}
		b.mu.Unlock()
		if cb, ok := taskbridge.TaskCallbackFromCtx(ctx); ok {
			cfg.OnStarted = func() {
				cb(protocol.StepEvent{
					Action:       protocol.StepActionSubAgentStart,
					SubAgentType: profile.Name, SubAgentProfile: profile.Name,
					SubAgentSkills: skillNames, SubAgentPrompt: spec.Prompt, SubAgentStatus: "running",
					SubAgentAttemptID: attempt.ID, SubAgentAttemptSeq: attempt.Seq,
				})
			}
		}
		cfg.SubagentID = id
		cfg.ResumeContext = restored
		cfg.SaveContext = func(snapshot ctxmgr.ManagerSnapshot, status, output string, usage subagent.AttemptUsage) error {
			return child.Checkpoint(snapshot, status, output, session.SubagentAttemptUsage{
				TotalTokens: usage.TotalTokens, CacheHitTokens: usage.CacheHitTokens, CacheMissTokens: usage.CacheMissTokens,
				ToolUseCount: usage.ToolUseCount, DurationMs: usage.DurationMs, ContextTokens: usage.ContextTokens, ContextWindow: usage.ContextWindow,
			})
		}
		result, err := engine.Run(ctx, cfg)
		if result != nil && (result.CacheHitTokens > 0 || result.CacheMissTokens > 0) {
			ctxMgr.RecordSubagent(result.TotalTokens, result.CacheHitTokens, result.CacheMissTokens)
		}
		converted := subagentTaskResult(result)
		if converted != nil {
			converted.ID = id
		}
		return converted, err
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
	attemptID string,
	attemptSeq int,
) subagent.RunConfig {
	cfg := subagent.RunConfig{
		Prompt:               spec.Prompt,
		Profile:              profile,
		SkillContents:        skillContents,
		ContextWindow:        contextWindow,
		AutoCompactPercent:   autoCompactPercent,
		ConfirmFn:            ag.ConfirmFn(),
		FullAccess:           ag.Executor().FullAccess,
		ConfigurePermissions: ag.Executor().ConfigureChildPermissions,
		ToolState:            ag.ToolExecutionState(),
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
		cfg.OnOutput = func(output subagent.OutputEvent) {
			subCB(protocol.StepEvent{Action: output.Action, Output: output.Text, SubAgentReasoning: output.Reasoning, SubAgentMessageSeq: output.MessageSeq, SubAgentAttemptID: attemptID, SubAgentAttemptSeq: attemptSeq})
		}

		cfg.OnToolCall = func(ev subagent.ToolCallEvent) {
			subCB(protocol.StepEvent{
				Action:   ev.Action,
				CallID:   ev.CallID,
				ToolName: ev.ToolName,
				ToolArgs: ev.ToolArgs, ToolInput: ev.ToolInput,
				Output:            ev.Output,
				Decision:          ev.Decision,
				IsError:           ev.IsError,
				SubAgentAttemptID: attemptID, SubAgentAttemptSeq: attemptSeq,
			})
		}
	}
	if subCB, ok := taskbridge.TaskCallbackFromCtx(ctx); ok {
		cfg.OnPhase = func(phase string) {
			subCB(protocol.StepEvent{Action: protocol.StepActionSubAgentUpdate, SubAgentStatus: "running", SubAgentPhase: phase, SubAgentAttemptID: attemptID, SubAgentAttemptSeq: attemptSeq})
		}
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
