package modelcatalog

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestReasoningOptionsRoundTripAndIsolation(t *testing.T) {
	catalog, err := Decode([]byte(`{"p":{"api":"https://service.example/v1","models":{"new":{"id":"new","modalities":{"output":["text"]},"limit":{"context":96000,"output":16000},"reasoning":true,"reasoning_options":[{"type":"toggle"},{"type":"effort","values":[null,"default","low","high","high","max"]},{"type":"budget_tokens","min":1024}]}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	limits, ok := catalog.Lookup("https://service.example/v1", "new")
	if !ok || limits.Reasoning == nil || !limits.Reasoning.Toggle || !limits.Reasoning.Budget || !reflect.DeepEqual(limits.Reasoning.Efforts, []string{"low", "high", "max"}) {
		t.Fatalf("lost metadata: %+v", limits.Reasoning)
	}
	raw, err := catalog.MarshalSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	restored, err := parseSnapshot(raw)
	if err != nil {
		t.Fatal(err)
	}
	copy, _ := restored.Lookup("https://service.example/v1", "new")
	if !reflect.DeepEqual(limits, copy) {
		t.Fatal("round trip changed capability")
	}
	copy.Reasoning.Efforts[0] = "none"
	copy.Reasoning.Toggle = false
	unchanged, _ := restored.Lookup("https://service.example/v1", "new")
	if unchanged.Reasoning.Efforts[0] != "low" || !unchanged.Reasoning.Toggle {
		t.Fatal("caller mutated immutable catalog")
	}
}

func TestReasoningOptionsDoNotInventEfforts(t *testing.T) {
	enabled, disabled := true, false
	for _, tc := range []struct {
		raw            string
		supported      *bool
		unknown        bool
		efforts        int
		toggle, budget bool
	}{
		{``, &enabled, true, 0, false, false},
		{`[{"type":"effort","values":["extreme"]}]`, &enabled, true, 0, false, false},
		{`[{"type":"unknown"}]`, &enabled, true, 0, false, false},
		{`[{"type":"toggle"}]`, &enabled, false, 0, true, false},
		{`[{"type":"budget_tokens","min":1024}]`, &enabled, false, 0, false, true},
		{``, &disabled, false, 0, false, false},
		{`[{"type":"effort","values":["high"]}]`, &disabled, true, 0, false, false},
		{`[{"type":"effort","values":["high"]}]`, nil, true, 0, false, false},
	} {
		got := parseReasoning(tc.supported, json.RawMessage(tc.raw))
		if (got == nil) != tc.unknown {
			t.Fatalf("raw=%s got=%+v unknown=%v", tc.raw, got, tc.unknown)
		}
		if got != nil && (len(got.Efforts) != tc.efforts || got.Toggle != tc.toggle || got.Budget != tc.budget) {
			t.Fatalf("invented effort: %+v", got)
		}
	}
}

func TestConflictingReasoningRecordsAreNotGuessed(t *testing.T) {
	c, err := Decode([]byte(`{"a":{"api":"https://service.example/v1","models":{"same":{"id":"same","modalities":{"output":["text"]},"limit":{"context":96000},"reasoning":true,"reasoning_options":[{"type":"effort","values":["low"]}]}}},"b":{"api":"https://service.example/v1","models":{"same":{"id":"same","modalities":{"output":["text"]},"limit":{"context":96000},"reasoning":true,"reasoning_options":[{"type":"effort","values":["high"]}]}}}}`))
	if err == nil {
		if _, ok := c.Lookup("https://service.example/v1", "same"); ok {
			t.Fatal("conflicting capability chosen")
		}
	}
}
