package task

import (
	"context"
	"encoding/json"
	"fmt"

	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/taskbridge"
	"nekocode/bot/extension/tool/runtime/toolutil"
	"nekocode/protocol"
)

const taskListOutputBudget = 64 * 1024

type TaskListTool struct {
	toolutil.SafeReadOnlyTool
	list taskbridge.TaskLister
}

func NewTaskListTool() *TaskListTool { return &TaskListTool{} }

func (t *TaskListTool) Wire(list taskbridge.TaskLister) { t.list = list }

func (t *TaskListTool) Name() string { return "task_list" }

func (t *TaskListTool) Description() string {
	return "List delegated tasks in the current parent session. Use items[].id as task.resume_id; never use latest_attempt.id, which identifies one execution attempt; recorded=true means saved context exists. Returns bounded summaries with profile, status, and latest attempt; does not scan other parent sessions."
}

func (t *TaskListTool) Parameters() []core.Parameter {
	return []core.Parameter{
		{Name: "query", Type: "string", Description: "Optional case-insensitive search over task ID, profile, and prompt."},
		{Name: "status", Type: "string", Description: "Optional exact task status filter, such as running, failed, completed, partial, or interrupted."},
		{Name: "offset", Type: "number", Description: "Zero-based result offset. Default 0."},
		{Name: "limit", Type: "number", Description: "Maximum tasks to return, from 1 to 100. Default 20."},
	}
}

func taskListIntArg(args map[string]any, name string, fallback int) (int, error) {
	raw, exists := args[name]
	if !exists {
		return fallback, nil
	}
	value, ok := raw.(float64)
	if !ok {
		return 0, fmt.Errorf("%s must be an integer", name)
	}
	converted := int(value)
	if value != float64(converted) {
		return 0, fmt.Errorf("%s must be an integer", name)
	}
	return converted, nil
}

func (t *TaskListTool) Execute(ctx context.Context, args map[string]any) (core.ToolOutput, error) {
	if t.list == nil {
		return core.ToolOutput{}, fmt.Errorf("task_list: not wired")
	}
	query, ok := args["query"].(string)
	if !ok {
		query = ""
	}
	status, ok := args["status"].(string)
	if !ok {
		status = ""
	}
	offset, err := taskListIntArg(args, "offset", 0)
	if err != nil {
		return core.ToolOutput{}, err
	}
	limit, err := taskListIntArg(args, "limit", 20)
	if err != nil {
		return core.ToolOutput{}, err
	}
	if offset < 0 || limit < 1 || limit > 100 {
		return core.ToolOutput{}, fmt.Errorf("offset must be non-negative and limit must be between 1 and 100")
	}
	page, err := t.list(ctx, taskbridge.TaskListFilter{Query: query, Status: status, Offset: offset, Limit: limit})
	if err != nil {
		return core.ToolOutput{}, err
	}
	for i := range page.Items {
		item := &page.Items[i]
		item.Title = taskListPreview(item.Title, 256)
		item.Prompt = taskListPreview(item.Prompt, 1024)
		item.Result = taskListPreview(item.Result, 4096)
		if item.LatestAttempt != nil {
			attempt := *item.LatestAttempt
			attempt.Prompt = taskListPreview(attempt.Prompt, 1024)
			attempt.Result = taskListPreview(attempt.Result, 4096)
			item.LatestAttempt = &attempt
		}
	}
	data, err := json.Marshal(page)
	if err != nil {
		return core.ToolOutput{}, err
	}
	if len(data) <= taskListOutputBudget {
		return core.ToolOutput{Text: string(data)}, nil
	}
	low, high := 0, len(page.Items)-1
	for low <= high {
		count := low + (high-low)/2
		candidate := page
		candidate.Items = page.Items[:count]
		candidate.NextOffset = offset + count
		candidate.HasMore = true
		candidateData, err := json.Marshal(candidate)
		if err != nil {
			return core.ToolOutput{}, err
		}
		if len(candidateData) <= taskListOutputBudget {
			data = candidateData
			low = count + 1
		} else {
			high = count - 1
		}
	}
	return core.ToolOutput{Text: string(data)}, nil
}

func taskListPreview(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	return protocol.SubagentTextPreview(text, limit) + "…[truncated]"
}
