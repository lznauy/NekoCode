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
	"nekocode/bot/extension/tool/builtin/catalog"
	"nekocode/bot/extension/tool/runtime/runner"
	"nekocode/bot/extension/tool/runtime/taskbridge"
	"nekocode/bot/extension/tool/runtime/workspace"
	"nekocode/bot/policy"
	"nekocode/bot/prompt"
	"nekocode/bot/provider"
	"nekocode/bot/session"
	"nekocode/bot/worktree"
	"nekocode/protocol"
	"time"
)

func (b *Bot) wireTaskTool(parentModel config.ModelConfig, compactionModel provider.LLM, ag *agentcore.Agent) {
	registry := b.toolbox.Registry
	ctxMgr := b.ctxMgr
	modelConfig := b.cfg.Clone()
	compactionWindow := effectiveModelWindow(b.cfg.ResolveModel(b.cfg.CompactionModel))
	autoCompactPercent := b.cfg.EffectiveAutoCompactPercent()

	b.toolbox.WireTaskRunner(func(ctx context.Context, spec taskbridge.TaskSpec) (taskResult *taskbridge.TaskResult, runErr error) {
		id := taskbridge.TaskIDFromCtx(ctx)
		savedProfile := ""
		savedIsolation := "shared"
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
			if metadata.Isolation != "" {
				savedIsolation = metadata.Isolation
			}
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
		mode := spec.Isolation
		if spec.ResumeID != "" {
			if mode == "" {
				mode = savedIsolation
			}
			if mode != savedIsolation {
				return nil, fmt.Errorf("cannot change task isolation on resume")
			}
		} else if mode == "" {
			mode = profile.Isolation
		}
		if mode == "" {
			mode = "shared"
		}
		if mode != "shared" && mode != "worktree" {
			return nil, fmt.Errorf("invalid isolation mode")
		}
		if profile.Isolation == "worktree" && mode != "worktree" {
			return nil, fmt.Errorf("profile requires worktree isolation")
		}
		selectedModel, err := resolveSubagentModel(&modelConfig, parentModel, profile.Model)
		if err != nil {
			return nil, err
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
		runRegistry := registry
		var isolated *catalog.Toolbox
		var workInfo worktree.Info
		if mode == "worktree" {
			lease, err := worktree.Acquire(b.sess.CurrentID(), id)
			if err != nil {
				return nil, err
			}
			defer lease.Close()
			workInfo, err = lease.Open(ctx, b.cwd, id, spec.ResumeID != "")
			if err != nil {
				return nil, fmt.Errorf("open isolated worktree: %w", err)
			}
			isolated = catalog.NewToolboxWithConfig(catalog.ToolboxConfig{
				Workspace: workspace.New(workInfo.Cwd, nil), ImageGenModels: modelConfig.ImageGenModels, ImageUnderstandModels: modelConfig.ImageUnderstandModels,
			})
			defer func() {
				if err := isolated.Close(); err != nil {
					runErr = errors.Join(runErr, fmt.Errorf("close isolated tools: %w", err))
				}
			}()
			isolated.WireInteraction(ag.ToolInteraction())
			isolated.SetSessionID(id)
			// Extensions may capture parent state; do not silently reuse them in another cwd.
			for _, name := range profile.Tools {
				if _, err := isolated.Registry.Get(name); err != nil {
					return nil, fmt.Errorf("tool %q does not support isolated worktrees", name)
				}
			}
			runRegistry = isolated.Registry
		}
		if err := child.SetIsolation(mode); err != nil {
			return nil, err
		}
		subLLM := provider.New(provider.Config{
			APIKey: selectedModel.APIKey, BaseURL: selectedModel.BaseURL, Model: selectedModel.Model, Protocol: selectedModel.Protocol,
			Reasoning: resolvedReasoning(selectedModel),
		})
		engine := subagent.New(subagent.Config{
			LLM: subLLM, Tools: runRegistry, CompactionModel: compactionModel,
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
		cfg := buildSubagentRunConfig(ctx, spec, profile, skillContents, effectiveModelWindow(selectedModel), autoCompactPercent, ag, b.sess.CurrentID(), b.environment, attempt.ID, attempt.Seq)
		if isolated != nil {
			cfg.IsolatedWorkspace = true
			home, _ := os.UserHomeDir()
			cfg.ConfigurePermissions = func(executor *runner.Executor) {
				ag.Executor().ConfigureChildPermissions(executor)
				executor.SetWorkspace(workInfo.Cwd, home)
				isolated.SetSandboxEngine(executor.SandboxEngine())
			}
			cfg.Environment = func() prompt.Environment {
				return prompt.Environment{Cwd: workInfo.Cwd, Roots: []prompt.Root{{Path: workInfo.Cwd, Access: "read-write"}}, ManagedProcesses: isolated.ManagedProcessSummary()}
			}
			cfg.Prompt += "\n[Worktree isolation] Work only in " + workInfo.Cwd + ". This checkout starts at " + workInfo.Base + "; parent uncommitted files are excluded. Leave results here for review. Do not apply or remove this worktree yourself."
		}
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
		cfg.CompactionWindow = compactionWindow
		cfg.SubagentID = id
		cfg.ResumeContext = restored
		cfg.SaveContext = func(snapshot ctxmgr.ManagerSnapshot, status, output string, usage subagent.AttemptUsage) error {
			return child.Checkpoint(snapshot, status, output, session.SubagentAttemptUsage{
				TotalTokens: usage.TotalTokens, CacheHitTokens: usage.CacheHitTokens, CacheMissTokens: usage.CacheMissTokens,
				ToolUseCount: usage.ToolUseCount, DurationMs: usage.DurationMs, ContextTokens: usage.ContextTokens, ContextWindow: usage.ContextWindow,
			})
		}
		started := false
		originalStarted := cfg.OnStarted
		cfg.OnStarted = func() {
			started = true
			ag.Governance().Observe(ctx, policy.SubagentStart, policy.Facts{ActorID: id, Tool: policy.ToolFacts{Args: map[string]any{"profile": profile.Name, "attempt_id": attempt.ID}}})
			if originalStarted != nil {
				originalStarted()
			}
		}
		result, err := engine.Run(ctx, cfg)
		if started {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
			status := "completed"
			if err != nil {
				status = "failed"
			}
			if ctx.Err() != nil {
				status = "interrupted"
			}
			ag.Governance().Observe(cleanup, policy.SubagentStop, policy.Facts{ActorID: id, Tool: policy.ToolFacts{Args: map[string]any{"profile": profile.Name, "attempt_id": attempt.ID, "status": status}}})
			cancel()
		}
		if result != nil && (result.CacheHitTokens > 0 || result.CacheMissTokens > 0) {
			ctxMgr.RecordSubagent(result.TotalTokens, result.CacheHitTokens, result.CacheMissTokens)
		}
		converted := subagentTaskResult(result)
		if converted != nil {
			converted.ID = id
			if isolated != nil {
				converted.Content += fmt.Sprintf("\n[Worktree retained: %s]\nReview with /worktree diff %s; apply with /worktree apply %s", workInfo.Path, id, id)
			}
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
		command, err := b.ext.ModelSkill(name)
		if err != nil {
			return nil, nil, fmt.Errorf("delegated skill %q: %w", name, err)
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

// resolveSubagentModel requires an explicit configured reference. Unknown
// profile references must never become raw provider model IDs.
func resolveSubagentModel(cfg *config.Config, parent config.ModelConfig, name string) (config.ModelConfig, error) {
	if name == "" {
		return parent, nil
	}
	model, ok := cfg.LookupModelConfig(name)
	if !ok {
		return config.ModelConfig{}, fmt.Errorf("unknown subagent model %q: use a configured providers[].models (provider/model)", name)
	}
	return model, nil
}
