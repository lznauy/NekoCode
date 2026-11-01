package contextmgr

import (
	"encoding/json"

	"nekocode/bot/extension/tool/runtime/refbridge"
	"nekocode/bot/provider/types"
)

// Evidence shares the existing reference budget; task references retain their
// separate 4 KiB allowance. Empty evidence does not shrink the context view.
func referenceMessages(references, evidence refbridge.Page) []types.Message {
	budget := contextReferencesBytes
	hasEvidence := evidence.SessionID != "" && (len(evidence.Evidence) > 0 || evidence.Unavailable || evidence.HasMore)
	if hasEvidence {
		budget /= 2
	}
	var out []types.Message
	if msg := boundedContextReferencesMessage(references, budget); msg.Content != "" {
		out = append(out, msg)
	}
	if hasEvidence {
		used := 0
		for _, msg := range out {
			used += len(msg.Content)
		}
		if msg := evidenceReferencesMessage(evidence, contextReferencesBytes-used); msg.Content != "" {
			out = append(out, msg)
		}
	}
	return out
}

func evidenceReferencesMessage(page refbridge.Page, budget int) types.Message {
	// Deliberately whitelist metadata: even a caller-supplied inspect page cannot
	// introduce receipt bodies, hashes, paths or command output into the prefix.
	type item struct {
		ID          string `json:"id"`
		Sequence    int    `json:"sequence"`
		Tool        string `json:"tool"`
		Status      string `json:"status"`
		HasArtifact bool   `json:"has_artifact,omitempty"`
	}
	projection := struct {
		SessionID   string `json:"session_id"`
		Items       []item `json:"items"`
		Before      int    `json:"before"`
		HasMore     bool   `json:"has_more"`
		Unavailable bool   `json:"unavailable,omitempty"`
	}{SessionID: page.SessionID, Items: []item{}, Unavailable: page.Unavailable}
	for _, ref := range page.Evidence {
		projection.Items = append(projection.Items, item{ref.ID, ref.Sequence, ref.Tool, ref.Status, ref.HasArtifact})
	}
	// Drop whole source records: two physical executions may share one sequence,
	// and an exclusive cursor must not skip the second execution.
	drop := func() {
		last := projection.Items[len(projection.Items)-1].Sequence
		for len(projection.Items) > 0 && projection.Items[len(projection.Items)-1].Sequence == last {
			projection.Items = projection.Items[:len(projection.Items)-1]
		}
	}
	for {
		projection.Before = 0
		if len(projection.Items) > 0 {
			projection.Before = projection.Items[len(projection.Items)-1].Sequence
		}
		projection.HasMore = page.HasMore || len(projection.Items) < len(page.Evidence)
		data, _ := json.Marshal(map[string]any{"context_data": struct {
			Kind      string `json:"kind"`
			Content   any    `json:"content"`
			Omitted   int    `json:"omitted"`
			QueryTool string `json:"query_tool"`
		}{"evidence_references", projection, len(page.Evidence) - len(projection.Items), "evidence"}})
		if len(projection.Items) <= 16 && len(data) <= budget {
			return types.Message{Role: "user", Source: types.MessageSourceContextData, Content: string(data)}
		}
		if len(projection.Items) == 0 {
			return types.Message{}
		}
		drop()
	}
}
