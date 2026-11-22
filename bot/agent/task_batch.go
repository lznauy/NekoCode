package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"strings"
	"sync"

	"github.com/google/uuid"

	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/taskbridge"
	"nekocode/protocol"
	"nekocode/util/secrets"
)

type batchItemResult struct {
	ID            string `json:"id"`
	Output        string `json:"output,omitempty"`
	TaskID        string `json:"task_id,omitempty"`
	AttemptID     string `json:"attempt_id,omitempty"`
	Status        string `json:"status"`
	Summary       string `json:"summary,omitempty"`
	DeliveryError string `json:"delivery_error,omitempty"`
}

func (r *toolRunner) dispatchTaskBatch(ctx context.Context, shared map[string]any, items []taskbridge.BatchItem, callback RunCallback) (core.ToolOutput, error) {
	calls := make([]core.ToolCallItem, len(items))
	for i, item := range items {
		args := maps.Clone(shared)
		detail, err := json.Marshal(item)
		if err != nil {
			return core.ToolOutput{}, err
		}
		args["title"] = item.ID
		args["prompt"] = fmt.Sprintf("Assigned item: %s\n\n%s\n\nItem details (JSON):\n%s", item.ID, shared["prompt"], detail)
		calls[i] = core.ToolCallItem{ID: uuid.NewString(), Name: "task", Args: args}
	}
	type outcome struct{ status, attempt string }
	var mu sync.Mutex
	ended := map[string]outcome{}
	observe := func(ev protocol.StepEvent) {
		if ev.Action == protocol.StepActionSubAgentEnd {
			mu.Lock()
			ended[ev.SubAgentID] = outcome{ev.SubAgentStatus, ev.SubAgentAttemptID}
			mu.Unlock()
		}
		if callback != nil {
			callback(ev)
		}
	}
	filtered, results, policies := r.dispatchToolCalls(ctx, calls, observe)
	emitResultCallbacks(calls, filtered.Blocked, results, callback)
	for _, h := range filtered.PreToolHints {
		r.agent.injectHint(h)
	}
	if filtered.Terminated {
		r.agent.run.hookStopped = true
	} else {
		r.applyPolicyResults(policies)
	}
	rows := make([]batchItemResult, len(items))
	for i, item := range items {
		row := batchItemResult{ID: item.ID, Output: item.Output, Status: "not_started"}
		decision := results[i].NonExecution
		if results[i].PermissionDenied || (decision.Outcome == "blocked" && (decision.Stage == "mode" || decision.Stage == "governance")) {
			row.Status = "denied"
		} else if cb, ok := calls[i].Args["_sub_callback"].(taskbridge.TaskCallback); ok && decision.Stage == "" {
			out := ended[cb.ID]
			row.Status = out.status
			if out.attempt != "" {
				row.TaskID, row.AttemptID = cb.ID, out.attempt
			}
		}
		// task_id already identifies the worker in the manifest.
		text := strings.TrimPrefix(results[i].EffectiveOutput(), "[Subagent ID: "+row.TaskID+"]\n")
		if results[i].Evidence.PreservationFailed {
			row.DeliveryError = protocol.SubagentTextPreview(results[i].Error, 320)
		} else {
			row.Summary = protocol.SubagentTextPreview(text, 320)
		}
		rows[i] = row
	}
	return batchReport(ctx, rows)
}

func batchReport(ctx context.Context, rows []batchItemResult) (core.ToolOutput, error) {
	data, err := json.Marshal(rows)
	// Executor delivery filters credentials before applying its byte budget.
	// The replacement can be longer than the original value.
	text := secrets.Redact(string(data))
	if err != nil || len(text) <= core.MaxOutputBytes {
		return core.ToolOutput{Text: text}, err
	}
	// Store the entire manifest before reducing the inline reply. One row per
	// line allows read to retrieve individual items even after context trimming.
	lines := make([]string, len(rows))
	for i, row := range rows {
		line, err := json.Marshal(row)
		if err != nil {
			return core.ToolOutput{}, err
		}
		lines[i] = string(line)
	}
	var output core.ToolOutput
	store := core.OutputStoreFromContext(ctx)
	if store == nil {
		err = fmt.Errorf("batch manifest output store unavailable")
	} else {
		output.Artifact.Path, err = store.PreserveOutput("[\n" + strings.Join(lines, ",\n") + "\n]")
	}
	if err != nil {
		// All workers have already settled. Returning a tool error here would
		// hide their outcomes and could prompt a duplicate execution.
		degraded := struct {
			ManifestError string             `json:"manifest_error"`
			Items         []indexedBatchItem `json:"items"`
		}{protocol.SubagentTextPreview("Could not save full batch manifest: "+err.Error(), 320), batchIndex(rows)}
		data, err = json.Marshal(degraded)
		return core.ToolOutput{Text: string(data)}, err
	}
	for i := range rows {
		rows[i].Summary = ""
		rows[i].Output = ""
	}
	data, err = json.Marshal(rows)
	if err != nil {
		return core.ToolOutput{}, err
	}
	text = secrets.Redact(string(data))
	if len(core.ArtifactHeader(output.Artifact))+len(text) > core.MaxOutputBytes {
		// Even the user-supplied identifiers can exceed the wire budget.
		// Index is one-based in both the submitted checklist and the artifact.
		data, err = json.Marshal(batchIndex(rows))
		text = string(data)
	}
	output.Text = text
	return output, err
}

// A runtime-issued task UUID is bounded even when the input and worker text are large.
type indexedBatchItem struct {
	Index          int    `json:"index"`
	TaskID         string `json:"task_id,omitempty"`
	Status         string `json:"status"`
	DeliveryFailed bool   `json:"delivery_failed,omitempty"`
}

func batchIndex(rows []batchItemResult) []indexedBatchItem {
	index := make([]indexedBatchItem, len(rows))
	for i, row := range rows {
		index[i] = indexedBatchItem{Index: i + 1, TaskID: row.TaskID, Status: row.Status, DeliveryFailed: row.DeliveryError != ""}
	}
	return index
}
