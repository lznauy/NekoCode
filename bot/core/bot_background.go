package core

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/google/uuid"
	"nekocode/bot/agent"
	"nekocode/bot/agent/subagent"
	"nekocode/bot/config"
	ctxmgr "nekocode/bot/contextmgr"
	"nekocode/bot/extension/agentprofile"
	"nekocode/bot/extension/mcp"
	tools "nekocode/bot/extension/tool"
	"nekocode/bot/extension/tool/builtin/capability"
	"nekocode/bot/extension/tool/builtin/catalog"
	"nekocode/bot/extension/tool/runtime/execution"
	"nekocode/bot/extension/tool/runtime/runner"
	"nekocode/bot/extension/tool/runtime/taskbridge"
	"nekocode/bot/extension/tool/runtime/workspace"
	"nekocode/bot/prompt"
	"nekocode/bot/provider"
	"nekocode/bot/session"
	"nekocode/bot/worktree"
	"nekocode/mode"
	"nekocode/protocol"
)

// submitBackgroundTask resolves all mutable parent dependencies before handing
// execution to Runtime. The execution closure owns no Bot or main Agent pointer.
func (b *Bot) submitBackgroundTask(ctx context.Context, spec taskbridge.TaskSpec, parent config.ModelConfig, configuration config.Config, compaction provider.LLM, ag *agent.Agent) (*taskbridge.TaskResult, error) {
	host, ok := protocol.BackgroundSubmitterFromContext(ctx)
	if !ok {
		return nil, fmt.Errorf("host does not support background tasks; use the standard runtime")
	}
	if spec.Profile != "" && spec.Profile != "executor" {
		return nil, fmt.Errorf("Super Agent can only submit dedicated executor tasks")
	}
	if len(spec.After)+len(spec.InputRefs) > 8 {
		return nil, fmt.Errorf("at most 8 combined result inputs and dependencies")
	}
	if spec.ResumeID != "" && len(spec.After) > 0 {
		return nil, fmt.Errorf("new dependencies require a new task; resumed result inputs may only narrow")
	}
	if spec.ResumeID == "" && spec.Isolation == "" && (len(spec.After) > 0 || len(spec.InputRefs) > 0) {
		return nil, fmt.Errorf("after/inputs share reports only, not workspace files. Choose isolation=shared, worktree or snapshot explicitly for a new report-processing task; use resume_id of the upstream task to inspect or verify its actual saved code. No task was started")
	}
	snapshotWorkspace := spec.Isolation == "snapshot"
	explicitShared := spec.Isolation == "shared"
	if snapshotWorkspace {
		if spec.ResumeID != "" {
			return nil, fmt.Errorf("snapshot is for new tasks; omit isolation on resume to reuse the saved workspace")
		}
		spec.Isolation = "worktree"
	}
	spec.Profile = "executor"
	parentID, cwd, home := b.sess.CurrentID(), b.cwd, b.home
	id := spec.ResumeID
	var savedGroup string
	var savedCeiling []string
	var updateSequenceFloor uint64
	if id != "" {
		metadata, err := session.ReadSubagentMetadata(parentID, id)
		if err != nil {
			return nil, err
		}
		if metadata.Profile == "" && metadata.LatestAttempt == nil {
			return nil, fmt.Errorf("task setup stopped before its first execution attempt; inspect the original setup failure and submit a new task with the corrected workspace settings")
		}
		if metadata.Profile != "executor" {
			return nil, fmt.Errorf("cannot resume legacy %s task in Super Agent; submit a new executor task", metadata.Profile)
		}
		if spec.InputRefs == nil {
			spec.InputRefs = slices.Clone(metadata.ResultInputs)
		} else {
			for _, ref := range spec.InputRefs {
				if !slices.Contains(metadata.ResultInputs, ref) {
					return nil, fmt.Errorf("cannot expand result inputs on resume; create a new executor task")
				}
			}
		}
		savedGroup = metadata.GroupID
		savedCeiling = metadata.ToolCeiling
		updateSequenceFloor = metadata.TaskUpdateSequence
		if spec.Profile == "" {
			spec.Profile = metadata.Profile
		}
		if spec.Profile != metadata.Profile {
			return nil, fmt.Errorf("cannot change profile on resume")
		}
		if spec.Isolation == "" {
			spec.Isolation = metadata.Isolation
		}
		if spec.Isolation != metadata.Isolation {
			return nil, fmt.Errorf("cannot change isolation on resume")
		}
		if spec.Skills == nil {
			spec.Skills = metadata.Skills
		}
	} else {
		id = uuid.NewString()
	}
	inputRefs, resultReader, resultContext, err := captureResultInputs(ctx, parentID, spec.InputRefs)
	if err != nil {
		return nil, err
	}
	profile, err := b.ext.AgentProfile(spec.Profile)
	if err != nil {
		return nil, err
	}
	if err := profile.CheckSource(ctx); err != nil {
		return nil, err
	}
	profile = agentprofile.Clone(profile)
	if resultReader != nil || len(spec.After) > 0 {
		profile.Tools = append(profile.Tools, "task_result")
	}
	if spec.Isolation == "" {
		spec.Isolation = profile.Isolation
	}
	// New general executors use independent working copies, including a private
	// snapshot when local changes must be reviewed. Explicit shared requests,
	// saved tasks and narrow readers retain their workspace contract.
	automaticIsolation := false
	schedulingNote := ""
	if explicitShared {
		schedulingNote = "explicit_shared"
	}
	if snapshotWorkspace {
		schedulingNote = "workspace_snapshot"
	}
	if spec.Isolation == "" && spec.ResumeID == "" && !spec.ReadOnly {
		for _, name := range spec.RequiredTools {
			if name == capability.ToolName || strings.HasPrefix(name, "mcp__") {
				schedulingNote = "mcp_requires_shared"
				break
			}
		}
		if schedulingNote == "" {
			schedulingNote = worktree.AutomaticIsolationReason(ctx, cwd)
			if schedulingNote == "uncommitted_changes" {
				if reason := worktree.SnapshotIsolationReason(ctx, cwd); reason == "" {
					spec.Isolation, snapshotWorkspace = "worktree", true
					schedulingNote = "workspace_snapshot"
					automaticIsolation = true
				} else {
					schedulingNote = reason
				}
			}
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if schedulingNote == "" {
				spec.Isolation = "worktree"
				automaticIsolation = true
			}
		}
	}
	if spec.Isolation == "" {
		spec.Isolation = "shared"
	}
	if spec.Isolation != "shared" && spec.Isolation != "worktree" {
		return nil, fmt.Errorf("invalid isolation")
	}
	if profile.Isolation == "worktree" && spec.Isolation != "worktree" {
		return nil, fmt.Errorf("profile requires worktree isolation")
	}
	names, contents, err := b.delegatedSkills(append(profile.Skills, spec.Skills...))
	if err != nil {
		return nil, err
	}
	selected, err := resolveSubagentModel(&configuration, parent, profile.Model)
	if err != nil {
		return nil, err
	}
	instructions := b.promptBuilder.BuildProjectInstructions()
	parentOutputs := ag.ToolExecutionState().OutputStore
	// A detached authority retains live permission revocation without retaining
	// main-turn callbacks, caches, usage counters or the conversation manager.
	authority := runner.NewExecutor(b.toolbox.Registry)
	ag.Executor().ConfigureChildPermissions(authority)
	b.mu.Lock()
	permissionAuthority := b.permissionAuthority
	b.mu.Unlock()
	permissionAuthority.ConfigureChildPermissionMode(authority)

	// Capture only a service binding; all Agent/toolbox state stays independent.
	mcpScope := b.ext.CaptureDelegatedMCP()
	probe := catalog.NewToolboxWithConfig(catalog.ToolboxConfig{Workspace: workspace.New(cwd, nil), ImageGenModels: configuration.ImageGenModels, ImageUnderstandModels: configuration.ImageUnderstandModels})
	defer probe.Close()
	probeReader := resultReader
	if probeReader == nil && len(spec.After) > 0 {
		probeReader = func(context.Context, taskbridge.ResultQuery) (taskbridge.ResultPage, error) {
			return taskbridge.ResultPage{}, fmt.Errorf("dependencies are not ready")
		}
	}
	bindSharedResultReader(probe, probeReader)
	if profile.RequiredRole == mode.Coordinator {
		names := probe.Registry.DelegatableNames()
		// Executor uses the capabilities supported by this independently created
		// toolbox, never arbitrary parent extension closures.
		if spec.Isolation != "worktree" {
			for _, identity := range mcpScope.Identities() {
				names = append(names, capability.TargetName(identity.Server, identity.Tool))
			}
			if len(mcpScope.Identities()) > 0 {
				names = append(names, capability.ToolName)
			}
		}
		if spec.ResumeID != "" {
			if savedCeiling == nil {
				return nil, fmt.Errorf("saved executor task has no capability ceiling; create a new task")
			}
			names = slices.DeleteFunc(names, func(name string) bool { return !slices.Contains(savedCeiling, name) })
		}
		names = slices.DeleteFunc(names, func(name string) bool { return !slices.Contains(profile.Tools, name) })
		slices.Sort(names)
		profile.Tools = names
	}
	if spec.ReadOnly {
		profile.Tools = slices.DeleteFunc(profile.Tools, func(name string) bool {
			entry, err := probe.Registry.Lookup(name)
			return err != nil || !entry.PlanAllowed
		})
	}
	mcpScope = mcpScope.Filter(func(identity mcp.ToolIdentity) bool {
		return slices.Contains(profile.Tools, capability.TargetName(identity.Server, identity.Tool))
	})
	if slices.Contains(profile.Tools, capability.ToolName) {
		if spec.Isolation == "worktree" {
			return nil, fmt.Errorf("MCP services cannot be rebound to an isolated worktree; delegate them as a separate shared task")
		}
		probe.Registry.RegisterWithOptions(capability.New(mcpScope), tools.RegistrationOptions{ResolveTarget: capability.ResolveTarget, DelegationAllowed: true})
	}
	// Assert requirements after applying the configured and saved ceilings.
	// This rejects impossible work before admission, session creation or model use.
	for _, name := range spec.RequiredTools {
		if !slices.Contains(profile.Tools, name) {
			return nil, fmt.Errorf("required tool %q is unavailable in this executor scope; no task was started. Approvals cannot add missing tools. Use general scope for commands; saved task ceilings cannot expand on resume", name)
		}
	}
	readOnly := true
	for _, name := range profile.Tools {
		if strings.HasPrefix(name, "mcp__") {
			matched := false
			for _, identity := range mcpScope.Identities() {
				if capability.TargetName(identity.Server, identity.Tool) == name {
					matched = true
					break
				}
			}
			if !matched {
				return nil, fmt.Errorf("MCP target %q is unavailable in the captured task scope", name)
			}
			readOnly = false
			continue
		}
		entry, err := probe.Registry.Lookup(name)
		if err != nil || !entry.DelegationAllowed {
			return nil, fmt.Errorf("tool %q has no trusted independent background execution binding", name)
		}
		if !entry.PlanAllowed {
			readOnly = false
		}
	}
	cfg := subagent.RunConfig{Profile: profile, Prompt: spec.Prompt + resultContext, SkillNames: names, SkillContents: contents, ProjectInstructions: instructions, SessionID: parentID, SubagentID: id,
		ContextWindow: effectiveModelWindow(selected), AutoCompactPercent: configuration.EffectiveAutoCompactPercent(), CompactionWindow: effectiveModelWindow(configuration.ResolveModel(configuration.CompactionModel))}
	coordination := protocol.CoordinationFromContext(ctx)
	group := coordination.GroupID
	if !coordination.Notification {
		if planID := b.ctxMgr.ActiveTodoPlanID(); planID != "" {
			group = planID
		}
	}
	if savedGroup != "" {
		if coordination.Notification && savedGroup != coordination.GroupID {
			return nil, fmt.Errorf("completion notification cannot resume task from another plan %q", savedGroup)
		}
		group = savedGroup
	}
	if group == "" {
		group = protocol.ExecutionFromContext(ctx).RunID
	}
	work := protocol.BackgroundWork{After: slices.Clone(spec.After), Prompt: spec.Prompt, UpdateSequenceFloor: updateSequenceFloor, ID: id, SessionID: parentID, GroupID: group, Title: spec.Title, Profile: profile.Name, ReadOnly: readOnly, Workspace: cwd}
	work.Isolation, work.SchedulingNote = spec.Isolation, schedulingNote
	if spec.Isolation == "worktree" {
		work.Workspace = cwd + "#worktree:" + id
	}
	work.Run = func(taskCtx context.Context, background protocol.BackgroundHost) (output string, runErr error) {
		// Dependencies are resolved only after admission to an execution slot. The
		// scheduler pins upstream attempts until this task has captured their results.
		taskRefs, taskReader, taskContext := inputRefs, resultReader, resultContext
		if len(spec.After) > 0 {
			refs := slices.Clone(spec.InputRefs)
			for _, upstream := range spec.After {
				page, err := session.ReadSubagentResult(taskCtx, parentID, taskbridge.ResultQuery{ID: upstream, Limit: 256})
				if err != nil {
					return "", fmt.Errorf("dependency result: %w", err)
				}
				refs = append(refs, page.Ref)
			}
			var err error
			taskRefs, taskReader, taskContext, err = captureResultInputs(taskCtx, parentID, refs)
			if err != nil {
				return "", err
			}
		}
		standard, _ := mode.Resolve(mode.Standard)
		taskCtx = mode.WithSnapshot(taskbridge.WithTaskID(taskCtx, id), standard.Snapshot)
		taskCtx = protocol.WithExecutionIdentity(taskCtx, protocol.ExecutionIdentity{RunID: "task_" + id, TaskID: id})
		child, err := session.OpenSubagent(parentID, id, cwd, spec.ResumeID != "")
		if err != nil {
			return "", err
		}
		defer child.Close()
		taskGroup := protocol.CoordinationFromContext(taskCtx).GroupID
		if taskGroup == "" {
			taskGroup = group
		}
		if err := child.SetGroupID(taskGroup); err != nil {
			return "", err
		}
		local := cfg
		local.Prompt = spec.Prompt + taskContext
		if source, ok := background.(protocol.BackgroundInputSource); ok {
			local.PendingUpdates = source.ApplyUpdates
		}
		if spec.ResumeID != "" {
			snapshot := child.Snapshot().ContextSnapshot()
			local.ResumeContext = &snapshot
		}
		target := cwd
		var lease *worktree.Lease
		var info worktree.Info
		if spec.Isolation == "worktree" {
			if automaticIsolation {
				check := worktree.AutomaticIsolationReason
				if snapshotWorkspace {
					check = worktree.SnapshotIsolationReason
				}
				if reason := check(taskCtx, cwd); reason != "" {
					return "", fmt.Errorf("automatic worktree preflight changed: %s; no worker model was started. Resubmit with isolation=shared to inspect the current working copy", reason)
				}
			}
			lease, err = worktree.Acquire(parentID, id)
			if err != nil {
				return "", err
			}
			defer lease.Close()
			if snapshotWorkspace {
				info, err = lease.OpenSnapshot(taskCtx, cwd, id)
			} else {
				info, err = lease.Open(taskCtx, cwd, id, spec.ResumeID != "")
			}
			if err != nil {
				return "", err
			}
			target = info.Cwd
			local.IsolatedWorkspace = true
			local.Prompt += "\n[Worktree isolation] Work only in " + target + ". Map file paths under the parent repository to their corresponding paths in this private checkout. Leave results for review; do not apply or remove the worktree."
			if info.Snapshot {
				local.Prompt += " This snapshot includes the parent's staged, unstaged and non-ignored untracked files, preserving its git status/diff for review. Ignored untracked files are excluded. The saved integration base is the copied state: /worktree diff/apply includes only changes you make after that snapshot."
			} else {
				local.Prompt += " Parent uncommitted changes are excluded."
			}
		}
		box := catalog.NewToolboxWithConfig(catalog.ToolboxConfig{Workspace: workspace.New(target, nil), ImageGenModels: configuration.ImageGenModels, ImageUnderstandModels: configuration.ImageUnderstandModels})
		defer func() { runErr = errors.Join(runErr, box.Close()) }()
		bindSharedResultReader(box, taskReader)
		if slices.Contains(profile.Tools, capability.ToolName) {
			box.Registry.RegisterWithOptions(capability.New(mcpScope), tools.RegistrationOptions{ResolveTarget: capability.ResolveTarget, DelegationAllowed: true})
		}
		box.SetSessionID(id)
		box.WireInteraction(background.Ask, nil)
		local.ConfirmFn = background.Confirm
		local.ConfigurePermissions = func(e *runner.Executor) {
			authority.ConfigureChildPermissions(e)
			e.SetWorkspace(target, home)
			box.SetSandboxEngine(e.SandboxEngine())
		}
		// Only the immutable output namespace is shared; caches/checkpoints and
		// parent model-usage callbacks belong to the foreground turn.
		local.ToolState = execution.NewExecutionState()
		local.ToolState.OutputStore = parentOutputs
		local.Environment = func() prompt.Environment {
			return prompt.Environment{Cwd: target, Roots: []prompt.Root{{Path: target, Access: "read-write"}}, ManagedProcesses: box.ManagedProcessSummary()}
		}
		if err := child.SetIsolation(spec.Isolation); err != nil {
			return "", err
		}
		workspaceKind := "shared"
		if spec.Isolation == "worktree" {
			workspaceKind = "head"
			if info.Snapshot {
				workspaceKind = "snapshot"
			}
		}
		if err := child.SetWorkspaceKind(workspaceKind); err != nil {
			return "", err
		}
		if info.Path != "" {
			if err := child.SetWorkspaceBaseline(info.Base, info.SourceHead); err != nil {
				return "", err
			}
			local.Prompt += fmt.Sprintf("\n[Workspace baseline] workspace_base=%s; source_head=%s. Compare saved Git baselines for incremental review; snapshot file timestamps are not original edit times. Ignored dependencies may be absent.", info.Base, info.SourceHead)
		}
		if profile.RequiredRole == mode.Coordinator {
			if err := child.SetToolCeiling(profile.Tools); err != nil {
				return "", err
			}
		}
		if err := child.SetResultInputs(taskRefs); err != nil {
			return "", err
		}
		if err := child.Begin(profile.Name, spec.Prompt, names, spec.Title); err != nil {
			return "", err
		}
		attempt := child.Snapshot().Subagent.CurrentAttempt()
		if attempt == nil {
			return "", fmt.Errorf("missing task attempt")
		}
		local.AttemptID = attempt.ID
		local.References = child.ReferenceSource()
		local.References.Skill = func(name string) (string, bool) {
			for i, n := range names {
				if n == name {
					return contents[i], true
				}
			}
			return "", false
		}

		local.SaveContext = func(snapshot ctxmgr.ManagerSnapshot, status, output string, usage subagent.AttemptUsage) error {
			if err := child.Checkpoint(snapshot, status, output, session.SubagentAttemptUsage{Summary: usage.Summary, TotalTokens: usage.TotalTokens, CacheHitTokens: usage.CacheHitTokens, CacheMissTokens: usage.CacheMissTokens, ToolUseCount: usage.ToolUseCount, DurationMs: usage.DurationMs, ContextTokens: usage.ContextTokens, ContextWindow: usage.ContextWindow}); err != nil {
				return err
			}
			if sink, ok := background.(protocol.BackgroundUsageSink); ok {
				return sink.Usage(protocol.BackgroundUsage{TotalTokens: usage.TotalTokens, CacheHitTokens: usage.CacheHitTokens, CacheMissTokens: usage.CacheMissTokens, ToolUseCount: usage.ToolUseCount, DurationMs: usage.DurationMs})
			}
			return nil
		}

		emit := func(ev protocol.StepEvent) {
			ev.SubAgentID = id
			ev.SubAgentProfile = profile.Name
			ev.SubAgentType = profile.Name
			ev.SubAgentAttemptID = attempt.ID
			ev.SubAgentAttemptSeq = attempt.Seq
			background.Step(ev)
		}
		local.OnStarted = func() {
			emit(protocol.StepEvent{Action: protocol.StepActionSubAgentStart, SubAgentPrompt: spec.Prompt, SubAgentSkills: names, SubAgentStatus: "running"})
		}
		local.OnOutput = func(o subagent.OutputEvent) {
			emit(protocol.StepEvent{Action: o.Action, Output: o.Text, SubAgentReasoning: o.Reasoning, SubAgentMessageSeq: o.MessageSeq})
		}
		local.OnPhase = func(phase string) {
			emit(protocol.StepEvent{Action: protocol.StepActionSubAgentUpdate, SubAgentStatus: "running", SubAgentPhase: phase})
		}
		local.OnToolCall = func(ev subagent.ToolCallEvent) {
			emit(protocol.StepEvent{Action: ev.Action, CallID: ev.CallID, ToolName: ev.ToolName, ToolArgs: ev.ToolArgs, ToolInput: ev.ToolInput, Output: ev.Output, IsError: ev.IsError, Evidence: ev.Evidence, NonExecution: ev.NonExecution, Decision: ev.Decision})
		}
		llm := provider.New(provider.Config{APIKey: selected.APIKey, BaseURL: selected.BaseURL, Model: selected.Model, Protocol: selected.Protocol, Reasoning: resolvedReasoning(selected)})
		result, err := subagent.New(subagent.Config{LLM: llm, Tools: box.Registry, CompactionModel: compaction}).Run(taskCtx, local)
		if result == nil {
			return "", errors.Join(err, fmt.Errorf("subagent returned no result"))
		}
		output = subagent.FormatResult(result)
		if info.Path != "" {
			output += fmt.Sprintf("\n[Worktree retained: %s; workspace_base=%s; source_head=%s; review and apply explicitly for task %s]", info.Path, info.Base, info.SourceHead, id)
		}
		status := "completed"
		if result.Status == subagent.StatusPartial {
			status = "partial"
		}
		if result.Status == subagent.StatusFailed || err != nil {
			status = "failed"
		}
		if taskCtx.Err() != nil {
			status = "interrupted"
		}
		emit(protocol.StepEvent{Action: protocol.StepActionSubAgentEnd, SubAgentStatus: status, Output: output, Summary: &result.Summary})
		if result.Status == subagent.StatusFailed && err == nil {
			err = fmt.Errorf("subagent failed")
		}
		return output, err
	}
	b.backgroundLeases.Add(1)
	var releaseOnce sync.Once
	work.Release = func() { releaseOnce.Do(b.releaseBackgroundLease) }
	receipt, err := host.SubmitBackground(ctx, work)
	if err != nil {
		work.Release()
		return nil, err
	}
	workspaceNote := " Isolation: " + spec.Isolation + "."
	if schedulingNote != "" && spec.Isolation == "shared" {
		workspaceNote += " Shared workspace reason: " + schedulingNote + "; writers in this workspace execute sequentially."
	}
	if snapshotWorkspace {
		workspaceNote += " Private snapshot includes staged, unstaged and non-ignored untracked files; the original working copy and its index remain unchanged."
	}
	if spec.Isolation == "worktree" {
		workspaceNote += " Results stay in the task worktree; review and apply explicitly with /worktree before claiming integration."
	}
	return &taskbridge.TaskResult{ID: receipt.ID, Status: taskbridge.TaskStatusAccepted, Content: "[Background task accepted] plan_id=" + receipt.GroupID + ". Status: " + receipt.Status + "." + workspaceNote + " This task retains its plan_id across resumes. Before submitting new follow-up tasks for this demand, select that saved plan using todo_write with plan_id and no todos when a saved checklist exists. This is an acceptance receipt, not completion. The runtime will deliver the result in a later coordinator turn. Do not wait or poll."}, nil
}

// BackgroundStatePath returns a trusted per-session sidecar location.
func (b *Bot) BackgroundStatePath(sessionID string) string {
	if sessionID == "" || filepath.Base(sessionID) != sessionID || sessionID == "." || sessionID == ".." || strings.ContainsAny(sessionID, "/\\") {
		return ""
	}
	return filepath.Join(b.home, ".nekocode", "sessions", sessionID, "background.json")
}
