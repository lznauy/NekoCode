package runner

import (
	"context"

	"nekocode/bot/extension/tool/runtime/core"
)

// PreparePreviews prepares previews with the registry's default authority.
func (e *Executor) PreparePreviews(calls []core.ToolCallItem) {
	e.PreparePreviewsContext(context.Background(), calls)
}

// PreparePreviewsContext stores previews using request-scoped authority.
func (e *Executor) PreparePreviewsContext(ctx context.Context, calls []core.ToolCallItem) {
	ctx = e.toolContext(ctx)
	for i, c := range calls {
		if calls[i].Args == nil {
			calls[i].Args = map[string]any{}
			c.Args = calls[i].Args
		}
		if preview, ok := e.registry.Preview(ctx, c.Name, c.Args); ok {
			calls[i].Args["_preview"] = preview
		}
	}
}

func (e *Executor) emitPreview(ctx context.Context, call core.ToolCallItem) {
	if call.Args == nil {
		call.Args = map[string]any{}
	}
	preview, _ := call.Args["_preview"].(string)
	if preview == "" {
		var ok bool
		preview, ok = e.registry.Preview(e.toolContext(ctx), call.Name, call.Args)
		if !ok {
			return
		}
		call.Args["_preview"] = preview
	}
	e.fnMu.RLock()
	pfn := e.previewFn
	e.fnMu.RUnlock()
	if pfn != nil {
		pfn(call.ID, call.Name, call.Args, preview, "")
	}
}
