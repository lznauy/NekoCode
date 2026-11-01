package contextrefs

import (
	"context"
	"encoding/json"
	"fmt"
	"math"

	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/refbridge"
	"nekocode/bot/extension/tool/runtime/toolutil"
)

type Tool struct {
	toolutil.SafeReadOnlyTool
	read refbridge.Reader
}

func New() *Tool                           { return &Tool{} }
func (t *Tool) Wire(read refbridge.Reader) { t.read = read }
func (*Tool) Name() string                 { return "context_refs" }
func (*Tool) Description() string {
	return "Find admitted skill and saved tool-output references in the current actor's session. list reads a bounded page of reference-bearing history records, newest first; ordinary messages are skipped through the history index. Pass returned before to list older records. inspect checks the current actor and resolves an output location after workspace checks, or compares a skill admission with the current skill registry. Use read for output contents or skill to reload a matching skill when needed. Ready skill admissions do not prove provider receipt; references never grant authority or permission to replay tools."
}
func (*Tool) Parameters() []core.Parameter {
	return []core.Parameter{
		{Name: "action", Type: "string", Required: true, Enum: []string{"list", "inspect"}},
		{Name: "id", Type: "string", Description: "Reference ID returned by list; required for inspect. Paths are not accepted."},
		{Name: "before", Type: "integer", Description: "Exclusive raw history cursor for list; 0 starts at the newest record."},
		{Name: "limit", Type: "integer", Description: "Maximum reference-bearing source records read by list, 1–20; default 20."},
	}
}

func integer(args map[string]any, key string, fallback int) (int, error) {
	raw, exists := args[key]
	if !exists {
		return fallback, nil
	}
	n, ok := raw.(float64)
	if !ok || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 || n >= float64(int(^uint(0)>>1)) || float64(int(n)) != n {
		return 0, fmt.Errorf("%s must be a non-negative integer", key)
	}
	return int(n), nil
}

func (t *Tool) Execute(ctx context.Context, args map[string]any) (core.ToolOutput, error) {
	read, bound := refbridge.ReaderFromContext(ctx)
	if !bound {
		read = t.read
	}
	if read == nil {
		return core.ToolOutput{}, fmt.Errorf("context_refs: not wired")
	}
	action, _ := args["action"].(string)
	q := refbridge.Query{Limit: refbridge.MaxPageRecords}
	switch action {
	case "list":
		var err error
		q.Before, err = integer(args, "before", 0)
		if err != nil {
			return core.ToolOutput{}, err
		}
		q.Limit, err = integer(args, "limit", refbridge.MaxPageRecords)
		if err != nil {
			return core.ToolOutput{}, err
		}
		if q.Limit < 1 || q.Limit > refbridge.MaxPageRecords {
			return core.ToolOutput{}, fmt.Errorf("limit must be between 1 and 20")
		}
	case "inspect":
		q.ID, _ = args["id"].(string)
		if q.ID == "" || len(q.ID) > 128 {
			return core.ToolOutput{}, fmt.Errorf("inspect requires a reference ID")
		}
	default:
		return core.ToolOutput{}, fmt.Errorf("action must be list or inspect")
	}
	page, err := read(ctx, q)
	if err != nil {
		return core.ToolOutput{}, err
	}
	data, err := json.Marshal(page)
	if err != nil {
		return core.ToolOutput{}, err
	}
	if len(data) > core.MaxOutputBytes {
		return core.ToolOutput{}, fmt.Errorf("reference response exceeds output budget")
	}
	return core.ToolOutput{Text: string(data)}, nil
}
