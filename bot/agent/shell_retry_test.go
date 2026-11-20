package agent

import (
	"strings"
	"testing"

	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/protocol"
)

func TestShellRetryFeedback(t *testing.T) {
	failure := func(args, exception string) core.ToolCallResult {
		return core.ToolCallResult{Name: "shell", Output: "status: failed\noutput:\nTraceback (most recent call last):\n  File \"/project/planner.py\", line 42, in run\n    raise error\n" + exception + ": private detail\n",
			Evidence: protocol.ToolEvidence{Executions: []protocol.ExecutionReceipt{{Tool: "shell", ArgumentsHash: args, Status: "error",
				Process: protocol.ProcessObservation{State: "exited", ExitCodeKnown: true, ExitCode: 1, OutputComplete: true}}}}}
	}
	first, second := failure("same", "KeyError"), failure("same", "ValueError")
	agent := new(Agent)
	observe := func(items ...core.ToolCallResult) { agent.observeShellRetry(items) }
	observe(first)
	observe(core.ToolCallResult{Name: "edit"})
	observe(second)
	if len(agent.run.pendingHints) != 1 || !strings.Contains(agent.run.pendingHints[0].Content, "KeyError at planner.py:42 → ValueError at planner.py:42") || strings.Contains(agent.run.pendingHints[0].Content, "private") {
		t.Fatalf("unexpected hints: %v", agent.run.pendingHints)
	}
	incomplete := failure("same", "TypeError")
	incomplete.Evidence.Executions[0].Process.OutputComplete = false
	for _, batch := range [][]core.ToolCallResult{{first}, {failure("other", "ValueError")}, {incomplete}, {first, second}, {core.ToolCallResult{Name: "shell"}}} {
		agent.run.reset()
		observe(first)
		observe(batch...)
		observe(first)
		if len(agent.run.pendingHints) != 0 {
			t.Fatalf("unexpected feedback for %v: %v", batch, agent.run.pendingHints)
		}
	}
	agent.run.reset()
	observe(second)
	if len(agent.run.pendingHints) != 0 {
		t.Fatal("comparison survived run reset")
	}
}
