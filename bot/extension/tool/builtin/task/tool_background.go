package task

import (
	"context"
	"fmt"
	"maps"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/toolutil"
	"nekocode/mode"
	"nekocode/protocol"
	"strings"
)

type SubmitTool struct{ TaskTool }

func NewSubmitTool() *SubmitTool                                    { return &SubmitTool{} }
func (*SubmitTool) Name() string                                    { return "task_submit" }
func (*SubmitTool) ExecutionMode(map[string]any) core.ExecutionMode { return core.ModeSequential }
func (*SubmitTool) Description() string {
	return "Submit independent work to background subagents. When multiple tasks are independent, submit them together in one tool-call batch; do not wait for one to finish before submitting its independent peers. Use after only for real dependencies. If all tasks need the same source discovery, submit one mapper first, then submit the independent reviews together with its exact task_result result_ref in inputs. For new tasks, declare required_tools; on resume, omit it to inherit the saved capability ceiling. Include shell for git/tests. Split work by distinct deliverables. A pending-delivery rejection applies only to that task; use task_result/task_list and resume it after its delivery settles. Other settled tasks can resume independently. The dedicated executor is fixed by the host; provide prompt and title, never a profile. Use resume_id to continue the same unfinished task; do not create a replacement or poll/wait. Available only in Super Agent mode."
}
func (t *SubmitTool) Parameters() []core.Parameter {
	parameters := t.TaskTool.Parameters()
	result := make([]core.Parameter, 0, len(parameters)-1)
	for _, p := range parameters {
		if p.Name == "profile" {
			continue
		}
		if p.Name == "resume_id" {
			p.Description = "Continue a saved executor task in this parent session by full task ID. Successful resume preserves and activates its saved plan_id for checklist updates and subsequent new tasks; failure leaves the active plan unchanged. Legacy explore/coder/custom tasks cannot be resumed through this tool. Omit skills to reuse saved skills. When the user asks to continue an interrupted recorded executor, pass its items[].id here; omitting resume_id creates a separate task and loses that continuity."
		}
		if p.Name == "skills" {
			p.Description = "Task-scoped workflow skills; they cannot expand the executor's host-configured tools."
		}
		if p.Name == "isolation" {
			p.Enum = []string{"shared", "worktree", "snapshot"}
			p.Description = "Optional shared, worktree or snapshot. Omit for automatic parallel isolation: clean Git uses a HEAD worktree; ordinary uncommitted changes use a private snapshot that includes staged, unstaged and non-ignored untracked files. Use snapshot explicitly for reviewing current local changes. Do NOT choose shared just because code is uncommitted: shared writers serialize. Use shared only when the original directory must be modified, required MCP cannot be rebound, or ignored local artifacts are essential. Explicit worktree uses HEAD only. Snapshot changes stay private; /worktree diff/apply includes only the worker's changes beyond its copied baseline. For a new task with after/inputs isolation is required; those references share reports, not files. Use resume_id to verify upstream code in its saved workspace. Omit isolation on resume."
		}
		result = append(result, p)
	}
	result = append(result, core.Parameter{Name: "tool_scope", Type: "string", Enum: []string{"general", "read_tools_only"}, Description: "Default general: normal executor tools, including shell, subject to approvals. Use general for read-only code reviews that need git or tests. read_tools_only removes shell, edits and MCP; choose it only for file reading/searching with no commands. This selects available tools, not whether the task should modify files."})
	result = append(result, core.Parameter{Name: "required_tools", Type: "array", Items: &core.Schema{Type: "string"}, Description: "Required for new tasks: essential tool names checked before a worker starts. Include shell for git, tests, lint, builds or other commands; use [] only if none are needed. On resume, omit to reuse the saved capability ceiling; supplying names can only narrow that ceiling. These are requirements, not permission grants."})
	result = append(result, core.Parameter{Name: "inputs", Type: "array", Items: &core.Schema{Type: "string"}, Description: "Optional up to 8 exact result_ref values returned by task_result. Shares only these completed attempt results with this worker, not other tasks or session files. Omit on resume to reuse saved inputs; resumed inputs may only narrow. Upstream tasks must already have terminal results. New tasks with inputs must choose isolation explicitly; to verify upstream file changes, resume that task instead."})
	result = append(result, core.Parameter{Name: "fork_from", Type: "string", Description: "Continue knowledge from a terminal executor task by full task ID, in a NEW task and explicitly chosen workspace (normally isolation=snapshot to test parent fixes). Captures its exact saved attempt result and a bounded handoff; does not copy its full tool history or import its worktree edits. The old task/workspace stays intact. Use resume_id instead when continuing in the same saved workspace. Cannot combine with resume_id or after. Execution tools cannot expand beyond the source ceiling; only explicitly shared results are readable."})
	result = append(result, core.Parameter{Name: "max_steps", Type: "integer", Description: "Optional positive integer, at most 150 and no higher than the configured profile limit. Caps worker loop steps in this attempt. At the limit ordinary tools stop; one final handoff call is allowed. Unfinished work or no valid handoff returns partial; a valid handoff with no unfinished work may complete. Prefer 30 for bounded regression/verification. Resume remaining work with a new bounded attempt when justified. Omit for 12 ordinary model turns, lowered by the profile ceiling."})
	result = append(result, core.Parameter{Name: "review_policy", Type: "string", Enum: []string{"immediate", "batch"}, Description: "Default batch for new independent tasks; omission on resume preserves its policy. Explicit continue/complete outcomes allow grouped delivery after pending batch peers settle, independently of verification quality. Blocked/decision outcomes, host errors, cancellation and approval/question waits bypass batching. Reports without outcome are delivered immediately. UI progress and dependency scheduling remain immediate; choose immediate for incremental decisions."})
	result = append(result, core.Parameter{Name: "after", Type: "array", Items: &core.Schema{Type: "string"}, Description: "Optional up to 8 already accepted task IDs in this parent session. Wait without occupying an execution slot; start only after every upstream completes successfully. Failed, partial or cancelled upstreams block execution. Their final reports are shared automatically, never workspace files. New tasks with after must choose isolation explicitly; to test upstream code, resume the source task instead. No forward references or cycles."})
	return result
}
func (t *SubmitTool) Execute(ctx context.Context, args map[string]any) (core.ToolOutput, error) {
	if s, ok := mode.FromContext(ctx); !ok || s.ID != mode.SuperAgent {
		return core.ToolOutput{}, fmt.Errorf("task_submit requires Super Agent mode")
	}
	if _, ok := protocol.BackgroundSubmitterFromContext(ctx); !ok {
		return core.ToolOutput{}, fmt.Errorf("host does not support background tasks")
	}
	if _, supplied := args["profile"]; supplied {
		return core.ToolOutput{}, fmt.Errorf("task_submit uses the dedicated executor; profile selection is not supported")
	}
	if raw, supplied := args["review_policy"]; supplied {
		value, valid := raw.(string)
		if !valid || (value != "immediate" && value != "batch") {
			return core.ToolOutput{}, fmt.Errorf("review_policy must be immediate or batch")
		}
	}
	if raw, supplied := args["fork_from"]; supplied {
		id, valid := raw.(string)
		if !valid || strings.TrimSpace(id) == "" {
			return core.ToolOutput{}, fmt.Errorf("fork_from must be a nonempty task ID")
		}
		if _, resume := args["resume_id"]; resume {
			return core.ToolOutput{}, fmt.Errorf("fork_from and resume_id are mutually exclusive")
		}
		if isolation, _ := args["isolation"].(string); isolation == "" {
			return core.ToolOutput{}, fmt.Errorf("fork_from requires explicit isolation for the new workspace")
		}
		if after, supplied := args["after"]; supplied && after != nil {
			return core.ToolOutput{}, fmt.Errorf("fork_from cannot combine with after; pass completed inputs explicitly")
		}
	}
	if raw, supplied := args["after"]; supplied && raw == nil {
		return core.ToolOutput{}, fmt.Errorf("after must be an array of task IDs")
	}
	prepared := make(map[string]any, len(args)+1)
	maps.Copy(prepared, args)
	prepared["profile"] = "executor"
	if _, supplied := args["review_policy"]; !supplied {
		if id, _ := args["resume_id"].(string); id == "" {
			prepared["review_policy"] = "batch"
		}
	}
	if _, supplied := args["read_only"]; supplied {
		return core.ToolOutput{}, fmt.Errorf("read_only is no longer supported: it removed shell even for read-only reviews. Omit it; use tool_scope=general and required_tools:[\"shell\"] for git/tests, with no-edit constraints in prompt. No task was started")
	}
	scope := "general"
	if raw, ok := args["tool_scope"]; ok {
		var valid bool
		scope, valid = raw.(string)
		if !valid || (scope != "general" && scope != "read_tools_only") {
			return core.ToolOutput{}, fmt.Errorf("tool_scope must be general or read_tools_only")
		}
	}
	readOnly := scope == "read_tools_only"
	resumeID, _ := args["resume_id"].(string)
	_, supplied := args["required_tools"]
	if !supplied && strings.TrimSpace(resumeID) == "" {
		return core.ToolOutput{}, fmt.Errorf("required_tools must be an array of essential tool names; include shell for git/tests, or [] when none are needed. No task was started")
	}
	if raw, exists := args["required_tools"]; exists && raw == nil {
		return core.ToolOutput{}, fmt.Errorf("required_tools must be an array")
	}
	required, err := stringSliceArg(args, "required_tools")
	if err != nil {
		return core.ToolOutput{}, err
	}
	if len(required) > 32 {
		return core.ToolOutput{}, fmt.Errorf("at most 32 required tools")
	}
	for _, name := range required {
		if strings.TrimSpace(name) == "" {
			return core.ToolOutput{}, fmt.Errorf("required_tools must contain non-empty tool names")
		}
		if readOnly && (name == "shell" || strings.HasPrefix(name, "mcp__") || name == "capability") {
			return core.ToolOutput{}, fmt.Errorf("tool_scope=read_tools_only excludes required tool %q; use general. No task was started", name)
		}
	}
	if raw, supplied := args["inputs"]; supplied && raw == nil {
		return core.ToolOutput{}, fmt.Errorf("inputs must be an array; omit it to reuse saved inputs")
	}
	inputs, err := stringSliceArg(args, "inputs")
	if err != nil {
		return core.ToolOutput{}, err
	}
	if _, supplied := args["inputs"]; supplied && inputs == nil {
		inputs = []string{}
	}
	if len(inputs) > 8 {
		return core.ToolOutput{}, fmt.Errorf("at most 8 result references may be shared")
	}
	return t.TaskTool.execute(ctx, prepared, readOnly, inputs)
}

type CancelTool struct{ toolutil.SafeReadOnlyTool }

func (*CancelTool) Name() string { return "task_cancel" }
func (*CancelTool) Description() string {
	return "Cancel a background task belonging to the current parent session by its task ID. Cancelling does not undo completed changes."
}
func (*CancelTool) Parameters() []core.Parameter {
	return []core.Parameter{{Name: "id", Type: "string", Required: true, Description: "Background task ID"}}
}
func (*CancelTool) Execute(ctx context.Context, args map[string]any) (core.ToolOutput, error) {
	if s, ok := mode.FromContext(ctx); !ok || s.ID != mode.SuperAgent {
		return core.ToolOutput{}, fmt.Errorf("task_cancel requires Super Agent mode")
	}
	host, ok := protocol.BackgroundSubmitterFromContext(ctx)
	if !ok {
		return core.ToolOutput{}, fmt.Errorf("host does not support background tasks")
	}
	id, err := toolutil.RequireStringArg(args, "id")
	if err != nil {
		return core.ToolOutput{}, err
	}
	if err := host.CancelBackground(ctx, id); err != nil {
		return core.ToolOutput{}, err
	}
	return core.ToolOutput{Text: "Cancellation requested for task " + id}, nil
}
