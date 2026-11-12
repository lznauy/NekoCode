package agent

import (
	"fmt"
	"strings"

	"nekocode/bot/policy"
	"nekocode/protocol"
)

// Failed checklist transactions must be reconciled even if no plan was created.
// Keep the correction budget across repeated failures, clearing it on success.
func (a *Agent) recordTodoUpdate(err error) {
	if err != nil {
		a.run.todoUpdateError = protocol.SubagentTextPreview(err.Error(), 512)
		return
	}
	a.run.todoUpdateError = ""
	a.run.todoUpdateChecks = 0
}

func (r *turnRunner) continueFailedTodoUpdate() bool {
	a := r.agent
	if a.getCtx().Err() != nil || a.run.todoUpdateError == "" || a.run.todoUpdateChecks >= 2 {
		return false
	}
	a.run.todoUpdateChecks++
	a.stream.discardText()
	a.clearFinalState()
	a.run.step++
	a.injectHint(&policy.Hint{Type: "todo_sync", Severity: "critical", Content: "[todo_sync] 上次 todo_write 未成功：" + a.run.todoUpdateError + "。请核对当前需求的 plan_id 和工具返回，修正后重试；不得修改无关计划或虚报完成。无法修复时明确说明清单未同步及原因。"})
	return true
}

// Every execution mode reconciles its active delivery checklist before closing.
// Accepted background work may yield without completing the whole plan.
func (r *turnRunner) continueUnfinishedPlan() bool {
	a := r.agent
	coordination := protocol.CoordinationFromContext(a.getCtx())
	if a.getCtx().Err() != nil || (!coordination.Notification && !a.run.todoTouched) {
		return false
	}
	status := a.deps.ctxMgr.Status()
	if !status.HasTasks || !status.TasksActionable || coordination.PendingTasks > 0 {
		return false
	}
	planID := a.deps.ctxMgr.ActiveTodoPlanID()
	queried := false
	if host, ok := protocol.BackgroundSubmitterFromContext(a.getCtx()); ok {
		if activity, ok := host.(protocol.BackgroundPlanStatus); ok {
			queried = true
			if activity.PendingBackgroundPlan(planID) > 0 {
				return false
			}
		}
	}
	if !queried && a.run.todoDispatched && a.run.todoDispatchPlan == planID {
		return false
	}
	if a.run.todoCheckedPlan != planID {
		a.run.todoCheckedPlan = planID
		a.run.todoCompleted = 0
		a.run.todoCompletionChecks = 0
	}
	todos := a.deps.ctxMgr.Todos()
	completed := protocol.CountCompleted(todos)
	if completed > a.run.todoCompleted {
		a.run.todoCompletionChecks = 0
	}
	a.run.todoCompleted = completed
	var remaining []string
	for _, item := range todos {
		if protocol.TodoActionable(item) {
			remaining = append(remaining, protocol.SubagentTextPreview(item.Content, 256))
		}
		if len(remaining) == 12 {
			break
		}
	}
	a.stream.discardText()
	a.clearFinalState()
	a.run.step++
	// Bound correction attempts across tool calls too: a model that cannot
	// follow the plan must surface the unfinished work instead of looping.
	if a.run.todoCompletionChecks >= 3 {
		a.run.err = fmt.Errorf("仍有未完成待办，自动推进未能继续：%s", strings.Join(remaining, "；"))
		return true
	}
	a.run.todoCompletionChecks++
	a.injectHint(&policy.Hint{Type: "todo_completion", Severity: "critical", Content: "[todo_completion] 当前步骤结束不代表用户需求完成。仍有待办：" + strings.Join(remaining, "；") + "。核对原需求与已有结果，用 todo_write 如实更新；按当前模式继续执行下一步、分析或必要验证。已完成工作不要重做，不要为结束而虚报完成或删除尚未交付的项。若确实受阻，将对应项标为 blocked 并说明原因，需要用户决定时再用 question。用户取消的项标为 cancelled 并说明原因。已有受理的后台任务时可以给进度说明并让出本轮。"})
	return true
}

// Blocked/cancelled steps remain visible in a legitimate partial closeout.
func (r *turnRunner) todoDispositionSummary(includeOpen bool) string {
	a := r.agent
	syncNotice := ""
	if a.run.todoUpdateError != "" {
		syncNotice = "\n\n清单同步失败：" + a.run.todoUpdateError + "。本次报告不能视为清单已完成。"
		includeOpen = true
	}
	if !includeOpen && !a.run.todoTouched && !protocol.CoordinationFromContext(a.getCtx()).Notification {
		return ""
	}
	var lines []string
	for _, item := range a.deps.ctxMgr.Todos() {
		label := ""
		switch item.Status {
		case "pending", "in_progress":
			if includeOpen {
				label = "未完成"
			}
		case "blocked":
			label = "受阻"
		case "cancelled":
			label = "已取消"
		}
		if label != "" {
			detail := label
			if item.Reason != "" {
				detail += "：" + protocol.SubagentTextPreview(item.Reason, 256)
			}
			lines = append(lines, fmt.Sprintf("- %s（%s）", protocol.SubagentTextPreview(item.Content, 256), detail))
		}
		if len(lines) == 12 {
			break
		}
	}
	if len(lines) == 0 {
		return syncNotice
	}
	return syncNotice + "\n\n待办未完成事项：\n" + strings.Join(lines, "\n")
}
