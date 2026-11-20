package core

import (
	"context"
	"fmt"
	"io"
	"nekocode/bot/config"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"nekocode/bot/agent"
	"nekocode/bot/command"
	"nekocode/bot/contextmgr"
	toolcore "nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/session"
	"nekocode/protocol"
)

func TestGoalCommandPersistsAndPreservesObjective(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	b := &Bot{sess: session.New(t.TempDir()), ctxMgr: contextmgr.New(contextmgr.Config{})}
	b.cmd = command.New(command.Deps{CtxMgr: b.ctxMgr})
	b.registerSessionCommands(b.cmd.Parser())
	cmd := func(input string) protocol.CommandResult {
		t.Helper()
		out, err := b.ExecuteCommand(t.Context(), input, nil)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	const objective = "让  搜索\n快一点"
	if r := cmd("/goal " + objective); r.Action != protocol.CommandContinue {
		t.Fatal(r)
	}
	id := b.sess.CurrentID()
	saved, err := b.sess.Load(id)
	if err != nil || saved.Goal == nil || saved.Goal.Objective != objective {
		t.Fatalf("fresh session: %v, %v", saved, err)
	}
	cmd("/goal refine 首先改善首次搜索")
	cmd("/GOAL  PAUSE")
	if g := b.sess.CurrentGoal(); g.Criteria == "" || g.Status != "paused" || g.Objective != objective {
		t.Fatal(g)
	}
	cmd("/goal resume")
	cmd("/goal accept")
	if b.sess.CurrentGoal().Status != "active" {
		t.Fatal("accepted before review")
	}
	g := b.sess.CurrentGoal()
	a := &goalAudit{goal: g}
	a.review("建议用 1 秒作为目标。\nGOAL DONE")
	a.finish(&agent.RunResult{StopReason: "completed", FinalOutput: "proposed threshold"}, nil, nil)
	if g.Status != "review" {
		t.Fatal(g)
	}
	if err := b.saveGoal(g); err != nil {
		t.Fatal(err)
	}
	cmd("/goal accept")
	if b.sess.CurrentGoal().Status != "completed" {
		t.Fatal("acceptance not persisted")
	}
	cmd("/goal clear")
	if b.sess.CurrentGoal() != nil {
		t.Fatal("goal survived clear")
	}
}

func TestGoalVerificationRequiresCurrentMatchingReceipt(t *testing.T) {
	const check = "go test ./..."
	const run = "run-1"
	const call = "call-1"
	input := toolcore.JSONArgs(map[string]any{"command": check})
	receipt := protocol.ExecutionReceipt{ID: "receipt", CallID: call, Tool: "shell", Execution: protocol.ExecutionIdentity{RunID: run}, ArgumentsHash: toolcore.EvidenceHash(input), Status: "success", Process: protocol.ProcessObservation{State: "exited", ExitCodeKnown: true}}
	for _, tc := range []struct {
		name     string
		change   func(*protocol.ExecutionReceipt)
		complete bool
	}{
		{"success", func(*protocol.ExecutionReceipt) {}, true},
		{"unknown exit", func(r *protocol.ExecutionReceipt) { r.Process.ExitCodeKnown = false }, false},
		{"failed", func(r *protocol.ExecutionReceipt) { r.Process.ExitCode = 1 }, false},
		{"another run", func(r *protocol.ExecutionReceipt) { r.Execution.RunID = "old" }, false},
		{"changed arguments", func(r *protocol.ExecutionReceipt) { r.ArgumentsHash = "other" }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := receipt
			tc.change(&r)
			a := goalAudit{goal: &session.Goal{Check: check}, runID: run}
			a.observe(protocol.StepEvent{Action: protocol.StepActionExecuteTool, ToolName: "shell", CallID: call, ToolInput: input, Evidence: protocol.ToolEvidence{Executions: []protocol.ExecutionReceipt{r}}})
			a.review("Tests passed")
			a.finish(&agent.RunResult{StopReason: "completed"}, nil, nil)
			if (a.goal.Status == "completed") != tc.complete {
				t.Fatal(a.goal)
			}
		})
	}
}

func TestGoalContinuationAndInterruption(t *testing.T) {
	g := &session.Goal{}
	a := goalAudit{external: true, goal: g, runID: "previous", progress: true}
	a.review("profiled slow path\nGOAL NEXT")
	a.finish(&agent.RunResult{FinalOutput: "profiled slow path", StopReason: "completed"}, nil, nil)
	if g.PendingFrom != "previous" || g.Attempts != 1 || g.Status != "active" {
		t.Fatal(g)
	}
	next := goalAudit{external: true, goal: g, runID: "next"}
	next.review("GOAL NEXT")
	next.finish(&agent.RunResult{StopReason: "completed"}, nil, nil)
	if g.Status != "blocked" || g.PendingFrom != "" {
		t.Fatal(g)
	}
	next.finish(&agent.RunResult{Interrupted: true}, context.Canceled, nil)
	if g.Status != "paused" {
		t.Fatal(g)
	}
}

func TestGoalAgentProposesCriteriaAndWaitsForAcceptance(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(data), "propose concrete acceptance criteria") {
			t.Error("goal absent from model context")
		}
		step := calls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		if step == 1 {
			fmt.Fprint(w, "data: "+`{"choices":[{"index":0,"delta":{"content":"Next examine the cold-start baseline.\nGOAL NEXT"}}]}`+"\n\ndata: [DONE]\n\n")
			return
		}
		if !strings.Contains(string(data), "Next examine the cold-start baseline.") {
			t.Error("in-run continuation lost the previous progress report")
		}
		fmt.Fprint(w, "data: "+`{"choices":[{"index":0,"delta":{"content":"I propose measuring first-search latency.\nGOAL PROPOSE: Measure first-search latency.\nGOAL REVIEW"}}]}`+"\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	cfg := config.Config{Active: "test/model", Providers: []config.ProviderConfig{{Name: "test", Protocol: "openai", APIKey: "fixture-secret", BaseURL: server.URL + "/v1", Models: []config.ModelDefinition{{Name: "model", Model: "gpt-4o-mini", ContextWindow: 128000}}}}}
	if err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	b, err := NewAt(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	host := &subagentDemoHost{}
	r, err := b.ExecuteCommand(t.Context(), "/goal Make search faster", host)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = b.Run(t.Context(), r.AgentInput, nil, host); err != nil {
		t.Fatal(err)
	}
	saved, err := b.sess.Load(b.CurrentSessionID())
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 || saved.Goal.Status != "review" || saved.Goal.Criteria != "" || saved.Goal.Proposal != "Measure first-search latency." || !strings.Contains(saved.Goal.Evidence, "latency") {
		t.Fatalf("calls=%d goal=%+v", calls.Load(), saved.Goal)
	}
}

func TestGoalProposalNeedsConfirmation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	b := &Bot{sess: session.New(t.TempDir()), ctxMgr: contextmgr.New(contextmgr.Config{})}
	b.cmd = command.New(command.Deps{CtxMgr: b.ctxMgr})
	g := &session.Goal{Objective: "improve performance", Status: "review", Proposal: "first search below 1 second"}
	if err := b.saveGoal(g); err != nil {
		t.Fatal(err)
	}
	out, err := b.ExecuteCommand(t.Context(), "/goal confirm", nil)
	if err != nil || out.Action != protocol.CommandContinue {
		t.Fatalf("%+v %v", out, err)
	}
	curr := b.sess.CurrentGoal()
	if curr.Objective != g.Objective || curr.Criteria != g.Proposal || curr.Proposal != "" {
		t.Fatal(curr)
	}
}

func TestGoalFinalCheckMustRunAloneAndSurviveUntilCloseout(t *testing.T) {
	const cmd = "go test ./..."
	a := goalAudit{goal: &session.Goal{Check: cmd}, runID: "run"}
	event := func(name string, args map[string]any, state string) protocol.StepEvent {
		input := toolcore.JSONArgs(args)
		r := protocol.ExecutionReceipt{ID: "physical-" + name, Execution: protocol.ExecutionIdentity{RunID: "run"}, Tool: name, CallID: "call", ArgumentsHash: toolcore.EvidenceHash(input), Status: "success", Process: protocol.ProcessObservation{Task: "job", State: state, ExitCodeKnown: state == "exited"}}
		return protocol.StepEvent{Action: protocol.StepActionExecuteTool, ToolName: name, CallID: "call", ToolInput: input, Evidence: protocol.ToolEvidence{Executions: []protocol.ExecutionReceipt{r}}}
	}
	shell := event("shell", map[string]any{"command": cmd}, "running")
	wait := event("process", map[string]any{"action": "wait", "task": "job"}, "exited")
	a.observe(shell)
	a.observe(wait)
	if a.receipt == "" {
		t.Fatal("verified process exit missing")
	}
	a.observe(event("edit", map[string]any{}, ""))
	if a.receipt != "" {
		t.Fatal("stale verification after edit")
	}
	a.observe(protocol.StepEvent{Action: protocol.StepActionToolStart})
	a.observe(protocol.StepEvent{Action: protocol.StepActionToolStart})
	a.observe(event("edit", map[string]any{}, ""))
	a.observe(event("shell", map[string]any{"command": cmd}, "exited"))
	if a.receipt != "" {
		t.Fatal("accepted parallel final check")
	}
	t.Run("adopting a process started before an edit", func(t *testing.T) {
		audit := goalAudit{goal: &session.Goal{Check: cmd}, runID: "run"}
		audit.observe(shell)
		audit.observe(event("edit", map[string]any{}, ""))
		reused := event("shell", map[string]any{"command": cmd}, "running")
		reused.Evidence.Executions[0].Process.Reason = "reused_running"
		audit.observe(reused)
		audit.observe(wait)
		if audit.receipt != "" {
			t.Fatal("accepted exit from a process launched before the edit")
		}
	})
	t.Run("execution-normalized arguments", func(t *testing.T) {
		audit := goalAudit{goal: &session.Goal{Check: cmd}, runID: "run"}
		audit.observe(event("shell", map[string]any{"command": " " + cmd + "\n"}, "running"))
		audit.observe(event("process", map[string]any{"action": " WAIT ", "task": "job"}, "exited"))
		if audit.receipt == "" {
			t.Fatal("missed the exit of the requested command")
		}
	})
}

func TestGoalProposalBelongsToLatestReview(t *testing.T) {
	for _, tt := range []struct{ label, answer, want string }{
		{"withdrawn", "Need your opinion.\nGOAL REVIEW", ""},
		{"new", "GOAL PROPOSE: 保留完整条件\nGOAL REVIEW", "保留完整条件"},
		{"long", "GOAL PROPOSE: " + strings.Repeat("条件", 400) + "，并保持兼容\nGOAL REVIEW", strings.Repeat("条件", 400) + "，并保持兼容"},
	} {
		t.Run(tt.label, func(t *testing.T) {
			g := &session.Goal{Proposal: "old"}
			a := goalAudit{goal: g}
			a.review(tt.answer)
			if g.Proposal != tt.want {
				t.Fatalf("proposal bytes=%d want %d", len(g.Proposal), len(tt.want))
			}
		})
	}
}

func TestGoalCloseWithoutReviewedAnswerPauses(t *testing.T) {
	g := &session.Goal{Evidence: "last confirmed observation"}
	a := goalAudit{goal: g}
	a.finish(&agent.RunResult{StopReason: "completed"}, nil, nil)
	if g.Status != "paused" || g.Evidence != "last confirmed observation" {
		t.Fatal(g)
	}
}

func TestGoalProgressExcludesBookkeepingAndEmptyPolls(t *testing.T) {
	for _, tc := range []struct {
		tool     string
		process  protocol.ProcessObservation
		progress bool
	}{
		{"todo_write", protocol.ProcessObservation{}, false},
		{"process", protocol.ProcessObservation{State: "running", UnchangedWaits: 2}, false},
		{"process", protocol.ProcessObservation{State: "running", NewOutputBytes: 10}, true},
		{"shell", protocol.ProcessObservation{State: "exited", ExitCodeKnown: true, ExitCode: 1}, true},
	} {
		a := goalAudit{goal: &session.Goal{}, runID: "run"}
		a.observe(protocol.StepEvent{Action: protocol.StepActionExecuteTool, ToolName: tc.tool, Evidence: protocol.ToolEvidence{Executions: []protocol.ExecutionReceipt{{ID: "receipt", Tool: tc.tool, Execution: protocol.ExecutionIdentity{RunID: "run"}, Process: tc.process}}}})
		if a.progress != tc.progress {
			t.Fatalf("%+v: progress=%v", tc, a.progress)
		}
	}
}

func TestGoalRepeatedPairAcrossRunsDoesNotEarnContinuation(t *testing.T) {
	g := &session.Goal{LastObservation: "read A first"}
	a := goalAudit{goal: g, runID: "run"}
	read := func(args, output string) bool {
		return a.observe(protocol.StepEvent{Action: protocol.StepActionExecuteTool, ToolName: "read", Evidence: protocol.ToolEvidence{
			Executions: []protocol.ExecutionReceipt{{ID: "id", Execution: protocol.ExecutionIdentity{RunID: a.runID}, Tool: "read", ArgumentsHash: args, OutputHash: output}},
		}})
	}
	if read("A", "first") || !read("B", "second") {
		t.Fatal("legacy observations lost or first read ignored")
	}
	a = goalAudit{goal: g, runID: "next"}
	if read("A", "first") || read("B", "second") || a.progress {
		t.Fatal("repeated pair counted as progress")
	}
	if !read("A", "changed") || !read("A", "first") {
		t.Fatal("actual changes ignored")
	}
	for i := 0; i < 100; i++ {
		read(fmt.Sprint(i), "output")
	}
	if n := len(g.RecentObservations); n != 64 {
		t.Fatalf("history length %d", n)
	}
}

func TestGoalProcessObservationIgnoresHandleAndTracksResult(t *testing.T) {
	g := &session.Goal{}
	for index, tt := range []struct {
		text string
		exit int
		want bool
	}{{"ok", 0, true}, {"ok", 0, false}, {"ok", 1, true}, {"new diagnostic", 1, true}} {
		p := protocol.ProcessObservation{State: "exited", ExitCodeKnown: true, ExitCode: tt.exit, OutputComplete: true, SampleHash: toolcore.EvidenceHash([]byte(tt.text))}
		p.Task = fmt.Sprintf("process_%d", index)
		r := protocol.ExecutionReceipt{Tool: "shell", ArgumentsHash: "args", Process: p, OutputHash: toolcore.OutputHash(toolcore.ToolOutput{Text: tt.text, Process: p})}
		if got := rememberGoalObservation(g, r); got != tt.want {
			t.Fatalf("%+v: got progress %v", tt, got)
		}
	}
}
