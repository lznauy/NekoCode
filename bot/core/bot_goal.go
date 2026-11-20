package core

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"unicode"

	"nekocode/bot/agent"
	"nekocode/bot/command"
	toolcore "nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/session"
	"nekocode/mode"
	"nekocode/protocol"
)

const goalContinuationLimit = 3

func goalSummary(g *session.Goal) string {
	switch g.Status {
	case "review":
		return "Goal awaits your review. Use /goal accept or /goal refine <criterion>."
	case "blocked":
		return "Goal stopped after a continuation produced no tool evidence. Use /goal to inspect progress."
	case "budget_limited":
		return "Goal reached its continuation limit. Use /goal resume to renew the budget."
	default:
		return "Goal [" + g.Status + "] " + g.Objective
	}
}

func goalStatus(g *session.Goal) string {
	if g == nil {
		return "No session goal. Use /goal <objective>."
	}
	s := fmt.Sprintf("Goal [%s] %s\nAutomatic continuations: %d/%d", g.Status, g.Objective, g.Attempts, goalContinuationLimit)
	if g.Status == "review" && g.Proposal != "" {
		s += "\nProposed criterion (use /goal confirm): " + g.Proposal
	}
	if g.Criteria != "" {
		s += "\nUser criterion: " + g.Criteria
	}
	if g.Check != "" {
		s += "\nAcceptance command: " + g.Check
	} else if g.Delegated {
		s += "\nAcceptance delegated to the agent."
	}
	if g.Evidence != "" {
		s += "\n" + g.Evidence
	}
	if g.Status == "review" {
		s += "\nUse /goal accept, /goal refine <criterion>, /goal check <command>, or /goal delegate."
	}
	return s
}

func (b *Bot) saveGoal(g *session.Goal) error {
	snapshot := b.captureCurrentSession()
	snapshot.Goal = g
	return b.persistSession(snapshot)
}

func splitGoalArg(s string) (string, string) {
	s = strings.TrimSpace(s)
	i := strings.IndexFunc(s, unicode.IsSpace)
	if i < 0 {
		return s, ""
	}
	return s[:i], strings.TrimSpace(s[i:])
}

func (b *Bot) goalCommand(ctx context.Context, cmd *command.Command) (protocol.CommandResult, error) {
	handled := func(text string) (protocol.CommandResult, error) {
		return protocol.CommandResult{Action: protocol.CommandHandled, Output: text}, nil
	}
	if err := ctx.Err(); err != nil {
		return protocol.CommandResult{}, err
	}
	_, arg := splitGoalArg(cmd.Raw)
	op, detail := splitGoalArg(arg)
	if protocol.IsGoalPauseCommand(cmd.Raw) {
		op = "pause"
	}
	g := b.sess.CurrentGoal()
	if arg == "" {
		return handled(goalStatus(g))
	}
	start := false
	switch op {
	case "clear":
		if detail != "" {
			return handled("Usage: /goal clear")
		}
		g = nil
	case "confirm", "accept", "pause", "resume", "delegate", "refine", "check":
		if g == nil {
			return handled(goalStatus(nil))
		}
		if op == "refine" || op == "check" {
			if detail == "" {
				return handled("Usage: /goal " + op + " <criterion>")
			}
		} else if detail != "" {
			return handled("Usage: /goal " + op)
		}
		switch op {
		case "accept":
			if g.Status != "review" {
				return handled("Only a goal awaiting review can be accepted.")
			}
			g.Status = "completed"
		case "pause":
			if g.Status == "completed" {
				return handled(goalStatus(g))
			}
			g.Status = "paused"
		default:
			if op == "confirm" {
				if g.Status != "review" || g.Proposal == "" {
					return handled("No proposed criterion awaits confirmation.")
				}
				detail = g.Proposal
				op = "refine"
			}
			if op == "refine" {
				if g.Criteria != "" {
					g.Criteria += "\n"
				}
				g.Criteria += detail
				g.Proposal = ""
				g.Check = ""
				g.Delegated = false
			}
			if op == "check" {
				g.Check = detail
				g.Delegated = false
			}
			if op == "delegate" {
				g.Delegated = true
				g.Check = ""
			}
			if op == "resume" && g.Status == "completed" {
				return handled(goalStatus(g))
			}
			start = true
		}
	default:
		if op == "set" {
			if detail == "" {
				return handled("Usage: /goal set <objective>")
			}
			arg = detail
		}
		g = &session.Goal{Objective: arg}
		start = true
	}
	if g != nil {
		g.PendingFrom = ""
	}
	if start {
		if b.InteractionMode() != mode.Standard {
			return handled("Select /mode standard to work on a goal.")
		}
		g.Status = "active"
		g.Attempts = 0
		g.LastObservation = ""
		g.RecentObservations = nil
	}
	if err := b.saveGoal(g); err != nil {
		return protocol.CommandResult{}, err
	}
	if start {
		return protocol.CommandResult{Action: protocol.CommandContinue, Output: goalStatus(g), AgentInput: "Work toward the active session goal: " + g.Objective}, nil
	}
	return handled(goalStatus(g))
}

type goalAudit struct {
	goal            *session.Goal
	runID           string
	receipt         string
	external        bool
	pending         bool
	task            string
	inflight        int
	batchParallel   bool
	settled         bool
	stalled         bool
	progress        bool
	requestedReview bool
	delegatedDone   bool
}

func (a *goalAudit) prompt() string {
	g := a.goal
	prompt := "Active user goal: " + g.Objective + "\nUser clarification: " + g.Criteria +
		"\nPreserve the user's objective and constraints. For a vague goal, do useful reversible work, propose concrete acceptance criteria and distinguish suggestions from agreed criteria. Record a proposed criterion on a separate line starting with GOAL PROPOSE: before GOAL REVIEW. Explain results and outstanding gaps. When further progress needs a user decision, end with GOAL REVIEW. For unfinished actionable work, end with GOAL NEXT. These markers must be on their own final line."
	if g.Check != "" {
		prompt += "\nThe user selected this acceptance command. Run it alone as the final shell tool, and verify exit status: " + g.Check
	}
	if g.Delegated {
		prompt += "\nThe user delegated acceptance. State the criteria applied and evidence. End with GOAL DONE only when all conditions hold."
	}
	if g.Evidence != "" {
		prompt += "\nPrevious goal review: " + g.Evidence
	}
	return prompt
}

// Keep one result per tool/input pair across internal and externally scheduled turns.
// Old snapshots carried only the last observation in the same wire format.
func rememberGoalObservation(g *session.Goal, receipt protocol.ExecutionReceipt) bool {
	const limit = 64
	if g.LastObservation != "" {
		if len(g.RecentObservations) == 0 {
			g.RecentObservations = []string{g.LastObservation}
		}
		g.LastObservation = ""
	}
	prefix := receipt.Tool + " " + receipt.ArgumentsHash + " "
	digest := receipt.OutputHash
	if p := receipt.Process; p.SampleHash != "" {
		// Audit receipts identify a physical execution. Goal progress identifies
		// a new result even when two launches use different process handles.
		digest = fmt.Sprintf("%s/%s/%t/%d/%t", p.SampleHash, p.State, p.ExitCodeKnown, p.ExitCode, p.OutputComplete)
	}
	observation := prefix + digest
	if receipt.ErrorHash != "" {
		observation += " " + receipt.ErrorHash
	}
	index := slices.IndexFunc(g.RecentObservations, func(s string) bool { return strings.HasPrefix(s, prefix) })
	changed := index < 0 || g.RecentObservations[index] != observation
	if index >= 0 {
		g.RecentObservations = slices.Delete(g.RecentObservations, index, index+1)
	}
	g.RecentObservations = append(g.RecentObservations, observation)
	if len(g.RecentObservations) > limit {
		g.RecentObservations = g.RecentObservations[len(g.RecentObservations)-limit:]
	}
	return changed
}

func (a *goalAudit) observe(e protocol.StepEvent) (newObservation bool) {
	if e.SubAgentID != "" {
		return
	}
	if e.Action == protocol.StepActionToolStart {
		a.receipt = ""
		if a.inflight == 0 {
			a.batchParallel = false
		} else {
			a.batchParallel = true
		}
		a.inflight++
		return
	}
	if e.Action != protocol.StepActionExecuteTool {
		return
	}
	if a.inflight > 0 {
		a.inflight--
	}
	a.receipt = ""
	previousTask := a.task
	a.task = ""
	if e.Evidence.PreservationFailed || e.NonExecution.ID != "" {
		return
	}
	if len(e.Evidence.Executions) == 0 {
		return
	}
	receipt := e.Evidence.Executions[len(e.Evidence.Executions)-1]
	if receipt.Execution.RunID != a.runID || receipt.Execution.TaskID != "" || receipt.ID == "" {
		return
	}
	// Bookkeeping and a still-running process with no new output are not new
	// task evidence. A real failed test can still inform the next action.
	unchangedWait := receipt.Tool == "process" && receipt.Process.State == "running" && receipt.Process.NewOutputBytes == 0
	if receipt.Tool != "todo_write" && !unchangedWait && rememberGoalObservation(a.goal, receipt) {
		a.progress = true
		newObservation = true
	}
	if a.batchParallel || a.goal.Check == "" || e.IsError || receipt.Tool != e.ToolName || receipt.CallID != e.CallID || receipt.ArgumentsHash != toolcore.EvidenceHash(e.ToolInput) {
		return
	}
	var args struct{ Command, Action, Task string }
	if json.Unmarshal(e.ToolInput, &args) != nil {
		return
	}
	matching := e.ToolName == "shell" && strings.TrimSpace(args.Command) == a.goal.Check
	if matching && receipt.Process.Reason == "reused_running" {
		// A repeated shell invocation can adopt an older live process. Its
		// eventual exit is valid only while the original launch is still tracked.
		matching = previousTask != "" && receipt.Process.Task == previousTask
	}
	if e.ToolName == "process" {
		action := strings.ToLower(strings.TrimSpace(args.Action))
		matching = previousTask != "" && args.Task == previousTask && receipt.Process.Task == previousTask && (action == "wait" || action == "watch")
	}
	if !matching {
		return
	}
	if receipt.Process.State == "running" {
		a.task = receipt.Process.Task
		return
	}
	if receipt.Status == "success" && receipt.Process.ExitCodeKnown && receipt.Process.State == "exited" && receipt.Process.ExitCode == 0 {
		a.receipt = receipt.ID
	}
	return
}

func (a *goalAudit) review(answer string) string {
	lines := strings.Split(strings.TrimSpace(answer), "\n")
	marker := agent.GoalMarker(answer)
	a.settled = true
	a.requestedReview = marker == "GOAL REVIEW"
	a.goal.Proposal = ""
	if a.requestedReview {
		var proposals []string
		for _, line := range lines[:len(lines)-1] {
			if proposal, ok := strings.CutPrefix(strings.TrimSpace(line), "GOAL PROPOSE:"); ok {
				if proposal = strings.TrimSpace(proposal); proposal != "" {
					proposals = append(proposals, proposal)
				}
			}
		}
		a.goal.Proposal = strings.Join(proposals, "\n")
	}
	a.delegatedDone = a.goal.Delegated && marker == "GOAL DONE" && len(lines) > 1
	if marker == "GOAL NEXT" {
		a.receipt = ""
	}
	if a.requestedReview || a.receipt != "" || a.delegatedDone {
		return ""
	}
	if marker != "GOAL NEXT" && a.goal.Check == "" {
		return ""
	}
	if a.goal.Attempts >= goalContinuationLimit || (!a.progress && a.goal.Attempts > 0) {
		a.stalled = true
		return ""
	}
	a.goal.Attempts++
	if a.external {
		a.pending = true
		return ""
	}
	a.settled = false
	a.progress = false
	return "Continue the active goal. Use the preceding report and tool results to address the next unmet criterion. If a check failed to run, distinguish the test setup failure from a product failure; retry when the next attempt tests a different explanation. If a permission, missing input or user preference blocks progress, report it and end with GOAL REVIEW."
}

func (a *goalAudit) finish(result *agent.RunResult, cancelErr, persistenceErr error) {
	g := a.goal
	g.PendingFrom = ""
	if a.settled && strings.TrimSpace(result.FinalOutput) != "" && !result.Interrupted {
		g.Evidence = protocol.SubagentTextPreview(result.FinalOutput, 2048)
	}
	switch {
	case !a.settled || result.Interrupted || cancelErr != nil || result.Error != nil || persistenceErr != nil || result.StopReason != "completed":
		g.Status = "paused"
	case a.pending:
		g.Status = "active"
		g.PendingFrom = a.runID
	case a.stalled && g.Attempts >= goalContinuationLimit:
		g.Status = "budget_limited"
	case a.stalled:
		g.Status = "blocked"
	case a.settled && !a.requestedReview && a.receipt != "":
		g.Status = "completed"
		g.Evidence = "Acceptance command exited 0. Execution receipt: " + a.receipt
	case a.delegatedDone:
		g.Status = "completed"
	default:
		g.Status = "review"
	}
}

// PendingGoalRun binds an automatic continuation to its immediately preceding
// run. Replacing, pausing or clearing the goal revokes the pending admission.
func (b *Bot) PendingGoalRun(previous string) bool {
	g := b.sess.CurrentGoal()
	return g != nil && g.Status == "active" && g.PendingFrom == previous && previous != ""
}

func (b *Bot) ActiveSessionGoal() bool { return b.goalRunning.Load() }
