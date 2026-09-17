package core

import (
	"encoding/json"
	"testing"
)

func TestJSONArgsPreservesStructuredAndUnderscoreFields(t *testing.T) {
	args := map[string]any{"_id": "user-field", "nested": map[string]any{"n": 3}, "_preview": "display", "_sub_id": "user-field", "_sub_callback": func() {}}
	var got map[string]any
	if err := json.Unmarshal(JSONArgs(args), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got["_id"] != "user-field" || got["_sub_id"] != "user-field" || got["nested"].(map[string]any)["n"] != float64(3) {
		t.Fatal(got)
	}
	if len(args) != 5 {
		t.Fatal("input mutated")
	}
}
