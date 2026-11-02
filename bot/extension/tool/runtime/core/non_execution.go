package core

import (
	"context"
	"time"

	"github.com/google/uuid"
	"nekocode/protocol"
)

// NotExecuted is called only by the host at a known pre-invocation boundary.
// A tool-returned error must retain its physical receipt instead.
func NotExecuted(ctx context.Context, call ToolCallItem, stage, reason string) ToolCallResult {
	name, _ := call.Effective()
	outcome := "blocked"
	if ctx.Err() != nil {
		outcome = "cancelled"
	}
	return ToolCallResult{ID: call.ID, Name: name, Error: reason, NonExecution: protocol.ToolNonExecution{
		Schema: 1, ID: uuid.NewString(), Execution: protocol.ExecutionFromContext(ctx), CallID: call.ID,
		Tool: name, Stage: stage, Outcome: outcome, ReasonHash: EvidenceHash([]byte(reason)), RecordedAt: time.Now().UnixNano(),
	}}
}
