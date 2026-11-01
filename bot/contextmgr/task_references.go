package contextmgr

import (
	"encoding/json"

	"nekocode/bot/provider/types"
	"nekocode/protocol"
)

const MaxTaskReferences = 16
const taskReferencesBytes = 4096

// TaskReferences is a request-only view, not task state or permission to resume.
// The owner resolves lifecycle and attempt ordering before supplying this value.
type TaskReferences struct {
	SessionID   string          `json:"session_id"`
	Items       []TaskReference `json:"items"`
	Total       int             `json:"total"`
	Unavailable bool            `json:"unavailable,omitempty"`
}

type TaskReference struct {
	ID         string `json:"id"`
	Recorded   bool   `json:"recorded"`
	Profile    string `json:"profile,omitempty"`
	Title      string `json:"title,omitempty"`
	Status     string `json:"status"`
	AttemptID  string `json:"attempt_id,omitempty"`
	AttemptSeq int    `json:"attempt_seq,omitempty"`
}

func (refs TaskReferences) message() types.Message {
	if refs.SessionID == "" {
		return types.Message{}
	}
	refs.Total = max(refs.Total, len(refs.Items))
	refs.Items = append([]TaskReference{}, refs.Items[:min(len(refs.Items), MaxTaskReferences)]...)
	for i := range refs.Items {
		refs.Items[i].Title = protocol.SubagentTextPreview(refs.Items[i].Title, 128)
		refs.Items[i].Profile = protocol.SubagentTextPreview(refs.Items[i].Profile, 64)
	}
	for {
		data, _ := json.Marshal(map[string]any{"context_data": struct {
			Kind      string         `json:"kind"`
			Content   TaskReferences `json:"content"`
			Omitted   int            `json:"omitted"`
			QueryTool string         `json:"query_tool"`
		}{"task_references", refs, max(0, refs.Total-len(refs.Items)), "task_list"}})
		if len(data) <= taskReferencesBytes {
			return types.Message{Role: "user", Source: types.MessageSourceContextData, Content: string(data)}
		}
		// IDs remain exact. Drop an oversized item instead of inventing a shortened ID.
		if len(refs.Items) == 0 {
			return types.Message{}
		}
		refs.Items = refs.Items[:len(refs.Items)-1]
	}
}
