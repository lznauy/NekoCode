package ledger

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestToolEventAuditProjection(t *testing.T) {
	args := map[string]any{"_sub_callback": func() {}, "_preview": "display", "_id": "user", "nested": map[string]any{"_preview": "legitimate nested field"}}
	for _, auditOnly := range []bool{false, true} {
		l := New()
		event := ToolEvent{Name: "task", Args: args, Error: "failed"}
		if auditOnly {
			l.RecordAuditTool(event)
		} else {
			l.RecordTool(event)
		}
		recorded := l.Snapshot().ToolErrors[0]
		if _, exists := recorded.Args["_sub_callback"]; exists {
			t.Fatal("runtime callback retained by ledger")
		}
		if _, exists := recorded.Args["_preview"]; exists {
			t.Fatal("preview retained by ledger")
		}
		if _, exists := args["_sub_callback"]; !exists {
			t.Fatal("live callback removed")
		}
		// Directly supplied snapshots must use the same durable projection.
		encoded, err := json.Marshal(Snapshot{ToolErrors: []ToolEvent{event}})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(encoded), "_sub_callback") {
			t.Fatalf("callback persisted: %s", encoded)
		}
		var restored Snapshot
		if err := json.Unmarshal(encoded, &restored); err != nil {
			t.Fatal(err)
		}
		got := restored.ToolErrors[0].Args
		if got["_id"] != "user" || got["nested"].(map[string]any)["_preview"] != "legitimate nested field" {
			t.Fatalf("user fields lost: %+v", got)
		}
	}
}
