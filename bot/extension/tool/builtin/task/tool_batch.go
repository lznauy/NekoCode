package task

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"unicode/utf8"

	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/taskbridge"
	"nekocode/bot/extension/tool/runtime/toolutil"
	"nekocode/bot/extension/tool/runtime/workspace"
)

// Sequential at the outer level: only the ordinary task executor owns worker slots.
type BatchTool struct{ toolutil.SequentialSafeTool }

func (*BatchTool) Name() string { return "task_batch" }

func (*BatchTool) Description() string {
	return "Delegate a fixed checklist of independent items with shared instructions in Standard mode. Use one item per URL, repository, document, or other target. Provide acceptance criteria in prompt. Each item gets an ordinary foreground task, using the configured worker pool. The parent receives one ordered summary after the batch settles. Output is an optional workspace-relative destination, not an access restriction. For edits, inspect the starting workspace and verify each deliverable against the criteria before reporting success. Use task_result for details and task with resume_id for unfinished work."
}

func (*BatchTool) Parameters() []core.Parameter {
	var params []core.Parameter
	for _, p := range (&TaskTool{}).Parameters() {
		if p.Name != "resume_id" && p.Name != "title" {
			if p.Name == "profile" {
				p.Required = true
			}
			params = append(params, p)
		}
	}
	return append(params, core.Parameter{Name: "items", Type: "array", Required: true, Description: "1–100 independent items; unique IDs and output paths.", Items: &core.Schema{Type: "object", Required: []string{"id", "input"}, Properties: map[string]core.Schema{
		"id":           {Type: "string", Description: "Stable short item identifier (up to 80 characters)."},
		"input":        {Type: "string", Description: "Source or target for this item."},
		"output":       {Type: "string", Description: "Optional unique workspace-relative output file."},
		"instructions": {Type: "string", Description: "Additional requirements for this item."},
	}}})
}

func (*BatchTool) Execute(ctx context.Context, args map[string]any) (core.ToolOutput, error) {
	dispatch, ok := args["_sub_callback"].(taskbridge.BatchDispatch)
	if !ok {
		return core.ToolOutput{}, fmt.Errorf("task_batch requires a parent agent")
	}
	shared := make(map[string]any)
	for key, v := range args {
		switch key {
		case "prompt", "profile", "skills", "max_steps", "isolation":
			shared[key] = v
		case "items", "_sub_callback", "_preview":
		default:
			return core.ToolOutput{}, fmt.Errorf("unknown task_batch parameter %q", key)
		}
	}
	for _, key := range []string{"prompt", "profile"} {
		if s, _ := shared[key].(string); strings.TrimSpace(s) == "" {
			return core.ToolOutput{}, fmt.Errorf("missing %s", key)
		}
	}
	if _, err := stringSliceArg(shared, "skills"); err != nil {
		return core.ToolOutput{}, err
	}
	if steps, err := taskStepArg(shared, "max_steps"); err != nil {
		return core.ToolOutput{}, err
	} else if steps > taskbridge.MaxRequestedSteps {
		return core.ToolOutput{}, fmt.Errorf("max_steps exceeds %d", taskbridge.MaxRequestedSteps)
	}
	if isolation, ok := shared["isolation"]; ok && isolation != "shared" && isolation != "worktree" {
		return core.ToolOutput{}, fmt.Errorf("invalid isolation")
	}
	// Count the submitted slice before making a second copy for schema decoding.
	list := reflect.ValueOf(args["items"])
	if (list.Kind() != reflect.Slice && list.Kind() != reflect.Array) || list.Len() == 0 || list.Len() > 100 {
		return core.ToolOutput{}, fmt.Errorf("items must contain 1–100 entries")
	}
	data, err := json.Marshal(args["items"])
	if err != nil {
		return core.ToolOutput{}, err
	}
	var items []taskbridge.BatchItem
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&items); err != nil {
		return core.ToolOutput{}, fmt.Errorf("invalid items: %w", err)
	}
	if len(items) == 0 || len(items) > 100 {
		return core.ToolOutput{}, fmt.Errorf("items must contain 1–100 entries")
	}
	manager, _ := workspace.FromContext(ctx)
	ids := map[string]bool{}
	var outputs []string
	var existing []os.FileInfo
	for i := range items {
		item := &items[i]
		item.ID = strings.TrimSpace(item.ID)
		if item.ID == "" || utf8.RuneCountInString(item.ID) > 80 || ids[item.ID] || strings.TrimSpace(item.Input) == "" {
			return core.ToolOutput{}, fmt.Errorf("item %d requires a unique short id and nonempty input", i+1)
		}
		ids[item.ID] = true
		if item.Output == "" {
			continue
		}
		if manager == nil || !filepath.IsLocal(item.Output) {
			return core.ToolOutput{}, fmt.Errorf("item %q requires a workspace-relative output file", item.ID)
		}
		dir, file := filepath.Split(item.Output)
		if file == "" || file == "." || file == ".." {
			return core.ToolOutput{}, fmt.Errorf("item %q requires an output file name", item.ID)
		}
		// Resolve normally harmless parent components before cleaning them.
		// A shell traverses a symlink before '..', while filepath.Join cleans
		// both components lexically. Dispatch only an unambiguous target.
		if end := strings.LastIndex(filepath.ToSlash(dir), "/../"); end >= 0 {
			prefix := dir[:end+3]
			physical, err := filepath.EvalSymlinks(manager.Primary() + string(filepath.Separator) + prefix)
			if err != nil {
				return core.ToolOutput{}, fmt.Errorf("item %q: cannot resolve output directory: %w", item.ID, err)
			}
			lexical, err := manager.Resolve(prefix)
			if err != nil || physical != lexical {
				return core.ToolOutput{}, fmt.Errorf("item %q has ambiguous output directory %q", item.ID, dir)
			}
		}
		path, err := manager.Resolve(item.Output)
		if err != nil {
			return core.ToolOutput{}, err
		}
		rel, err := filepath.Rel(manager.Primary(), path)
		if err != nil || rel == "." || !filepath.IsLocal(rel) {
			return core.ToolOutput{}, fmt.Errorf("item %q has conflicting or out-of-workspace output %q", item.ID, item.Output)
		}
		for _, other := range outputs {
			// Relative comparison follows platform path rules, including on Windows.
			to, _ := filepath.Rel(other, path)
			from, _ := filepath.Rel(path, other)
			if filepath.IsLocal(to) || filepath.IsLocal(from) {
				return core.ToolOutput{}, fmt.Errorf("conflicting output %q", item.Output)
			}
		}
		info, err := os.Stat(path)
		if err == nil {
			if !info.Mode().IsRegular() {
				return core.ToolOutput{}, fmt.Errorf("output %q is not a regular file", item.Output)
			}
			for _, previous := range existing {
				if os.SameFile(info, previous) {
					return core.ToolOutput{}, fmt.Errorf("duplicate output %q", item.Output)
				}
			}
			existing = append(existing, info)
		} else if !os.IsNotExist(err) {
			return core.ToolOutput{}, err
		}
		outputs = append(outputs, path)
	}
	return dispatch(ctx, shared, items)
}
