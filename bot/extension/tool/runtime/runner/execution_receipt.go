package runner

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/protocol"
)

// observeExecution wraps only actual invocations, including privileged retries.
// The receipt belongs to this caller; tools cannot supply or replace its ID.
func observeExecution(ctx context.Context, call core.ToolCallItem, invoke func() (core.ToolOutput, error)) (output core.ToolOutput, err error, receipt protocol.ExecutionReceipt) {
	name, args := call.Effective()
	receipt = protocol.ExecutionReceipt{ID: uuid.NewString(), Execution: protocol.ExecutionFromContext(ctx), CallID: call.ID, Tool: name, StartedAt: time.Now().UnixNano()}
	if input := core.JSONArgs(args); input != nil {
		receipt.ArgumentsHash = core.EvidenceHash(input)
	}
	panicked := false
	defer func() {
		if value := recover(); value != nil {
			panicked = true
			err = fmt.Errorf("panic: %v", value)
		}
		receipt.EndedAt = time.Now().UnixNano()
		receipt.Process, receipt.Artifact = output.Process, output.Artifact
		receipt.RawComplete = output.Process.OutputComplete && !output.Artifact.Incomplete
		receipt.OutputHash = core.OutputHash(output)
		receipt.Status = executionStatus(output, err)
		if panicked {
			receipt.Status = "unknown"
		}
		if err != nil {
			receipt.ErrorHash = core.EvidenceHash([]byte(err.Error()))
		}
	}()
	output, err = invoke()
	return
}

func executionStatus(output core.ToolOutput, err error) string {
	var permission core.PermissionError
	switch {
	case errors.As(err, &permission):
		return "blocked"
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return "cancelled"
	case err != nil:
		return "error"
	}
	switch output.Process.State {
	case "":
		return "success" // nil tool error only; process/business outcome unknown.
	case "exited":
		if !output.Process.ExitCodeKnown {
			return "unknown"
		}
		if output.Process.ExitCode != 0 {
			return "error"
		}
		return "success"
	case "killed", "timeout":
		return "cancelled"
	default:
		return "unknown"
	}
}
