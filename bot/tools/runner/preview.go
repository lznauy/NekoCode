package runner

import "nekocode/bot/tools/core"

// PreparePreviews runs Preview() on each mutable tool call and stores the
// result in Args["_preview"].
func (e *Executor) PreparePreviews(calls []core.ToolCallItem) {
	for i, c := range calls {
		if calls[i].Args == nil {
			calls[i].Args = map[string]any{}
			c.Args = calls[i].Args
		}
		if t, err := e.registry.Get(c.Name); err == nil {
			if p, ok := t.(Previewer); ok {
				calls[i].Args["_preview"] = p.Preview(c.Args)
			}
		}
	}
}

func (e *Executor) emitPreview(call core.ToolCallItem) {
	if t, err := e.registry.Get(call.Name); err == nil {
		if p, ok := t.(Previewer); ok {
			if call.Args == nil {
				call.Args = map[string]any{}
			}
			preview, _ := call.Args["_preview"].(string)
			if preview == "" {
				preview = p.Preview(call.Args)
				call.Args["_preview"] = preview
			}
			e.fnMu.RLock()
			pfn := e.previewFn
			e.fnMu.RUnlock()
			if pfn != nil {
				pfn(call.Name, call.Args, preview)
			}
		}
	}
}
