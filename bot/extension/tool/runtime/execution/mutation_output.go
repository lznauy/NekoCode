package execution

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"nekocode/bot/extension/tool/runtime/core"
)

// MutationOutput records knowledge of a version produced by this actor. Like a
// read receipt, it grants nothing until its exact result is in the next model
// request. Observe/Invalidate/Reset retain the same delivery and version rules.
func MutationOutput(ctx context.Context, path string, content []byte, output core.ToolOutput) core.ToolOutput {
	// A syntax check on the exact written bytes can satisfy simple JSON edits.
	// Incomplete documents are still writable; the receipt describes the result.
	if strings.EqualFold(filepath.Ext(path), ".json") {
		if utf8.Valid(content) && json.Valid(content) {
			output.Text += "\nJSON syntax: valid."
		} else {
			output.Text += "\nJSON syntax: invalid (file saved)."
		}
	}
	output.ReadID = FileReadsFromContext(ctx).Record(path, content, output.Render())
	return output
}
