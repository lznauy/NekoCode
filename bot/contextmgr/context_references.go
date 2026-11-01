package contextmgr

import (
	"encoding/json"

	"nekocode/bot/extension/tool/runtime/refbridge"
	"nekocode/bot/provider/types"
)

const contextReferencesBytes = 4096

// Context references share an 8 KiB ceiling with the separate 4 KiB task view.
// This is a bounded window of reference-bearing records, not a full inventory.
func contextReferencesMessage(page refbridge.Page) types.Message {
	return boundedContextReferencesMessage(page, contextReferencesBytes)
}

func boundedContextReferencesMessage(page refbridge.Page, budget int) types.Message {
	if page.SessionID == "" {
		return types.Message{}
	}
	// Only reference values enter the cache prefix. Source-page scan counts
	// and moving raw-window boundaries must not invalidate unchanged history.
	count := len(page.Items)
	projection := struct {
		SessionID   string                `json:"session_id"`
		Items       []refbridge.Reference `json:"items"`
		Before      int                   `json:"before"`
		HasMore     bool                  `json:"has_more"`
		Unavailable bool                  `json:"unavailable,omitempty"`
	}{SessionID: page.SessionID, Items: []refbridge.Reference{}, HasMore: page.HasMore, Unavailable: page.Unavailable}
	skills, artifacts := 0, 0
	for _, ref := range page.Items {
		if ref.Kind == "skill" {
			if skills == 8 {
				break
			}
			skills++
		} else {
			if artifacts == 16 {
				break
			}
			artifacts++
		}
		projection.Items = append(projection.Items, ref)
	}
	for {
		// Anchor older queries to the oldest retained reference, not the moving
		// recent-history window. A page with no references can restart list at 0.
		if len(projection.Items) > 0 {
			projection.Before = projection.Items[len(projection.Items)-1].Sequence
			projection.HasMore = page.HasMore || len(projection.Items) < count
		} else {
			projection.Before = 0
			projection.HasMore = page.HasMore || count > 0
		}
		data, _ := json.Marshal(map[string]any{"context_data": struct {
			Kind      string `json:"kind"`
			Content   any    `json:"content"`
			Omitted   int    `json:"omitted"`
			QueryTool string `json:"query_tool"`
		}{"context_references", projection, count - len(projection.Items), "context_refs"}})
		if len(data) <= budget {
			return types.Message{Role: "user", Source: types.MessageSourceContextData, Content: string(data)}
		}
		if len(projection.Items) == 0 {
			return types.Message{}
		}
		projection.Items = projection.Items[:len(projection.Items)-1]
	}
}
