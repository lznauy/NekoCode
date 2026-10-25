// Package toolpolicy adapts deterministic policy checks to tool calls for
// both the main-agent and sub-agent execution paths.
package toolpolicy

import (
	"context"
	"os"

	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/toolutil"
	"nekocode/bot/policy"
)

const defaultBlockReason = "blocked by policy"

// Decision is the complete PreToolUse projection for one tool call.
type Decision struct {
	Terminated  bool
	StopReason  policy.StopReason
	BlockReason string
	Hints       []policy.Hint
}

// Check evaluates one governance handle using the call's effective identity.
func Check(gov *policy.Policy, call core.ToolCallItem, contexts ...context.Context) Decision {
	if gov == nil {
		return Decision{}
	}
	name, args := call.Effective()
	request := policy.ToolRequest{Name: name, Args: args, TargetExists: targetExists(name, args)}
	results := gov.BeforeTool(request, contexts...)

	outcome := policy.Resolve(results)
	decision := Decision{Hints: outcome.Hints, Terminated: outcome.Action == policy.Terminate, StopReason: outcome.StopReason}
	if outcome.Action != policy.Continue {
		decision.BlockReason = outcome.Reason
		if decision.BlockReason == "" {
			decision.BlockReason = defaultBlockReason
		}
	}

	return decision
}

func targetExists(name string, args map[string]any) bool {
	if name != "write" && name != "edit" {
		return false
	}
	path, _ := args["path"].(string)
	if path == "" {
		return false
	}
	resolved, err := toolutil.ValidatePath(path)
	if err != nil {
		return false
	}
	_, err = os.Stat(resolved)
	return err == nil
}
