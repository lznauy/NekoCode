package subagent

import (
	"fmt"
	"strings"
	"testing"

	"nekocode/bot/extension/tool/runtime/core"
)

func handoffArgs() map[string]any {
	return map[string]any{"summary": "done", "evidence": []any{}, "files": []any{}, "verification": "not run", "unfinished": []any{}, "risks": []any{}}
}

func TestHandoffEvidenceIDsAreOptionalBoundedAndCloned(t *testing.T) {
	id := "evidence.2." + strings.Repeat("a", 64)
	for _, value := range []any{nil, []any{id}} {
		args := handoffArgs()
		if value != nil {
			args["evidence_ids"] = value
		}
		handoff, submitted, err := submittedResult([]core.ToolCallItem{{Name: submitResultToolName, Args: args}})
		if err != nil || !submitted {
			t.Fatalf("%+v %v", handoff, err)
		}
		if value != nil {
			result := buildHandoffResult(handoff, runMeta{})
			handoff.EvidenceIDs[0] = "mutation"
			if result.Handoff.EvidenceIDs[0] != id || !strings.Contains(result.Content, id) {
				t.Fatal("lost handoff IDs")
			}
		}
	}
	tooMany := make([]any, MaxEvidenceIDs+1)
	for i := range tooMany {
		tooMany[i] = fmt.Sprintf("evidence.%d.%s", i+1, strings.Repeat("b", 64))
	}
	for _, value := range []any{nil, "not array", []any{id, id}, []any{"/private/path"}, []any{"evidence.00." + strings.Repeat("a", 64)}, tooMany} {
		args := handoffArgs()
		args["evidence_ids"] = value
		if _, _, err := submittedResult([]core.ToolCallItem{{Name: submitResultToolName, Args: args}}); err == nil {
			t.Fatalf("accepted invalid IDs: %v", value)
		}
	}
}
