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

func NewSubmitTool() *SubmitTool { return &SubmitTool{} }
func (*SubmitTool) Name() string { return "task_submit" }
func (*SubmitTool) Description() string {
	return "Submit independent work to a background subagent. Declare required_tools before dispatch; missing capabilities are rejected before a worker starts. For git commands, tests, or code review requiring commands, include shell and keep tool_scope=general, even when files must not be modified. Returns an acceptance receipt immediately, not a completion result. Runtime delivers the completed result in a later coordinator turn. The dedicated executor is fixed by the host; provide prompt and title, never a profile. When continuing the same unfinished work, use resume_id from task_list instead of creating a replacement. Interrupted executor tasks retain saved context; resume_id continues that task with its history in a new attempt. Do not poll or wait. Available only in Super Agent mode."
}
func (t *SubmitTool) Parameters() []core.Parameter {
	parameters := t.TaskTool.Parameters()
	result := make([]core.Parameter, 0, len(parameters)-1)
	for _, p := range parameters {
		if p.Name == "profile" {
			continue
		}
		if p.Name == "resume_id" {
			p.Description = "Continue a saved executor task in this parent session by full task ID. Legacy explore/coder/custom tasks cannot be resumed through this tool. Omit skills to reuse saved skills. When the user asks to continue an interrupted recorded executor, pass its items[].id here; omitting resume_id creates a separate task and loses that continuity."
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
	result = append(result, core.Parameter{Name: "required_tools", Type: "array", Required: true, Items: &core.Schema{Type: "string"}, Description: "Tool names essential to this task, checked before starting a worker. Include shell for git, tests, lint, builds or other commands; include read/grep as needed. Use [] only if no tools are required. These are requirements, not permission grants or a tool whitelist."})
	result = append(result, core.Parameter{Name: "inputs", Type: "array", Items: &core.Schema{Type: "string"}, Description: "Optional up to 8 exact result_ref values returned by task_result. Shares only these completed attempt results with this worker, not other tasks or session files. Omit on resume to reuse saved inputs; resumed inputs may only narrow. Upstream tasks must already have terminal results. New tasks with inputs must choose isolation explicitly; to verify upstream file changes, resume that task instead."})
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
	if raw, supplied := args["after"]; supplied && raw == nil {
		return core.ToolOutput{}, fmt.Errorf("after must be an array of task IDs")
	}
	prepared := make(map[string]any, len(args)+1)
	maps.Copy(prepared, args)
	prepared["profile"] = "executor"
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
	rawRequired, supplied := args["required_tools"]
	if !supplied || rawRequired == nil {
		return core.ToolOutput{}, fmt.Errorf("required_tools must be an array of essential tool names; include shell for git/tests, or [] when none are needed. No task was started")
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
