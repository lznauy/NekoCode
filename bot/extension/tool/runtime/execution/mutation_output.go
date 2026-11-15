package execution

import (
	"context"

	"nekocode/bot/extension/tool/runtime/core"
)

// MutationOutput records knowledge of a version produced by this actor. Like a
// read receipt, it grants nothing until its exact result is in the next model
// request. Observe/Invalidate/Reset retain the same delivery and version rules.
func MutationOutput(ctx context.Context, path string, content []byte, output core.ToolOutput) core.ToolOutput {
	output.ReadID = FileReadsFromContext(ctx).Record(path, content, output.Render())
	return output
}
