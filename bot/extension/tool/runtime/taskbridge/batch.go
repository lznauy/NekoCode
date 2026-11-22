package taskbridge

import (
	"context"

	"nekocode/bot/extension/tool/runtime/core"
)

// BatchItem identifies one independently verifiable deliverable.
type BatchItem struct {
	ID           string `json:"id"`
	Input        string `json:"input"`
	Output       string `json:"output,omitempty"`
	Instructions string `json:"instructions,omitempty"`
}

type BatchDispatch func(context.Context, map[string]any, []BatchItem) (core.ToolOutput, error)
