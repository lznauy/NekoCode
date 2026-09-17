package protocol

// ToolInputArgs copies actual tool input without display metadata or runtime
// callbacks. Other underscore-prefixed keys remain legitimate user arguments.
func ToolInputArgs(args map[string]any) map[string]any {
	out := make(map[string]any, len(args))
	for key, value := range args {
		if key != "_preview" && key != "_sub_callback" {
			out[key] = value
		}
	}
	return out
}

// ApprovalArgs preserves the serializable preview needed by approval UIs while
// excluding runtime callbacks. Neither projection mutates execution arguments.
func ApprovalArgs(args map[string]any) map[string]any {
	out := ToolInputArgs(args)
	if preview, ok := args["_preview"].(string); ok {
		out["_preview"] = preview
	}
	return out
}
