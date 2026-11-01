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
	kind string
}

func NewEvidence() *Tool { return &Tool{kind: refbridge.EvidenceKind} }

func New() *Tool                           { return &Tool{} }
func (t *Tool) Wire(read refbridge.Reader) { t.read = read }
func (t *Tool) Name() string {
	if t.kind == refbridge.EvidenceKind {
		return "evidence"
	}
	return "context_refs"
}
func (t *Tool) Description() string {
	if t.kind == refbridge.EvidenceKind {
		return "Query recorded physical tool execution receipts in your own session or subtask. The parent may select a task_id and attempt_id belonging to its current session; children cannot select another scope. list scans at most 20 raw history records per page, newest first; an empty page may still have has_more. Pass before to continue. inspect accepts an evidence reference returned by list. Recorded success without process facts means only a nil tool error; it never proves tests passed or a business task completed. Results contain metadata only, never artifact paths, file contents, read permission or authority to replay tools."
	}
	return "Find admitted skill and saved tool-output references in the current actor's session. list reads a bounded page of reference-bearing history records, newest first; ordinary messages are skipped through the history index. Pass returned before to list older records. inspect checks the current actor and resolves an output location after workspace checks, or compares a skill admission with the current skill registry. Use read for output contents or skill to reload a matching skill when needed. Ready skill admissions do not prove provider receipt; references never grant authority or permission to replay tools."
}
func (t *Tool) Parameters() []core.Parameter {
	limitDescription := "Maximum reference-bearing source records read by list, 1–20; default 20."
	if t.kind == refbridge.EvidenceKind {
		limitDescription = "Maximum raw history records scanned by list, 1–20; default 20. Empty pages can have has_more."
	}
	parameters := []core.Parameter{
		{Name: "action", Type: "string", Required: true, Enum: []string{"list", "inspect"}},
		{Name: "id", Type: "string", Description: "Reference ID returned by list; required for inspect. Paths are not accepted."},
		{Name: "before", Type: "integer", Description: "Exclusive raw history cursor for list; 0 starts at the newest record."},
		{Name: "limit", Type: "integer", Description: limitDescription},
	}
	if t.kind == refbridge.EvidenceKind {
		parameters = append(parameters,
			core.Parameter{Name: "task_id", Type: "string", Description: "Parent actor only: saved subtask ID in this parent session. Must be paired with attempt_id."},
			core.Parameter{Name: "attempt_id", Type: "string", Description: "Parent actor only: exact saved attempt ID of task_id. Omit both for own history."})
	}
	return parameters
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
		return core.ToolOutput{}, fmt.Errorf("%s: not wired", t.Name())
	}
	action, _ := args["action"].(string)
	q := refbridge.Query{Limit: refbridge.MaxPageRecords, Kind: t.kind}
	if t.kind == refbridge.EvidenceKind {
		for key, target := range map[string]*string{"task_id": &q.TaskID, "attempt_id": &q.AttemptID} {
			if raw, exists := args[key]; exists {
				value, ok := raw.(string)
				if !ok || len(value) > 128 {
					return core.ToolOutput{}, fmt.Errorf("%s must be a bounded string", key)
				}
				*target = value
			}
		}
		if (q.TaskID == "") != (q.AttemptID == "") {
			return core.ToolOutput{}, fmt.Errorf("task_id and attempt_id must be paired")
		}
	}
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
