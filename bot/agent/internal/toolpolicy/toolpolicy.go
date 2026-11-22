// Package toolpolicy adapts deterministic policy checks to tool calls for
// both the main-agent and sub-agent execution paths.
package toolpolicy

import (
	"context"
	"os"

	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/workspace"
	"nekocode/bot/policy"
	"nekocode/bot/provider/types"
)

const defaultBlockReason = "blocked by policy"

// Decision is the complete PreToolUse projection for one tool call.
type Decision struct {
	Terminated  bool
	StopReason  policy.StopReason
	BlockReason string
	ReasonCode  string
	Hints       []policy.Hint
}

// Check evaluates one governance handle using the call's effective identity.
func Check(ctx context.Context, gov *policy.Policy, manager *workspace.Manager, call core.ToolCallItem) Decision {
	if gov == nil {
		return Decision{}
	}
	name, args := call.Effective()
	request := policy.ToolRequest{Name: name, Args: args, TargetExists: targetExists(manager, name, args)}
	results := gov.BeforeTool(request, ctx)

	outcome := policy.Resolve(results)
	decision := Decision{Hints: outcome.Hints, Terminated: outcome.Action == policy.Terminate, StopReason: outcome.StopReason}
	if outcome.Action != policy.Continue {
		decision.ReasonCode = outcome.ReasonCode
		decision.BlockReason = outcome.Reason
		if decision.BlockReason == "" {
			decision.BlockReason = defaultBlockReason
		}
	}

	return decision
}

func targetExists(manager *workspace.Manager, name string, args map[string]any) bool {
	if name != "write" && name != "edit" {
		return false
	}
	path, _ := args["path"].(string)
	if path == "" {
		return false
	}
	resolved, err := manager.Resolve(path)
	if err != nil {
		return false
	}
	_, err = os.Stat(resolved)
	return err == nil
}

// Message projects a tool delivery into model history for either agent.
func Message(r core.ToolCallResult) types.Message {
	return types.Message{Content: r.TextOutput().Text, ToolOutput: r.Artifact, ToolCallID: r.ID, IsError: r.Error != "", ReadID: r.ReadID, ToolEvidence: r.Evidence, NonExecution: r.NonExecution}
}

func Outcome(name string, args map[string]any, r core.ToolCallResult) policy.ToolResult {
	result := policy.ToolResult{Name: name, Args: core.PublicArgs(args), Output: r.Output, Error: r.Error, Process: r.Process}
	if r.NonExecution.Stage != "" {
		result.Blocked = true
		result.BlockReason = r.Error
	}
	return result
}
