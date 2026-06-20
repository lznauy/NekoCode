package taskwire

import (
	"nekocode/bot/agent/subagent"
	"nekocode/bot/tools"
)

func ToTaskResult(result *subagent.Result) *tools.TaskResult {
	if result == nil {
		return nil
	}
	status := tools.TaskStatusCompleted
	switch result.Status {
	case subagent.StatusFailed:
		status = tools.TaskStatusFailed
	case subagent.StatusPartial:
		status = tools.TaskStatusPartial
	}
	return &tools.TaskResult{
		Status:  status,
		Content: subagent.FormatResult(result),
	}
}
