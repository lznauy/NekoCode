package config

import (
	"bytes"
	"encoding/json"
	"testing"
)

func visionPtr(v bool) *bool { return &v }

func TestEffectiveVisionRespectsExplicitOverride(t *testing.T) {
	cases := []struct {
		model    string
		vision   *bool
		expected bool
	}{
		// Explicit values override the built-in table in both directions.
		{"glm-5.3", visionPtr(false), false},
		{"deepseek-v4", visionPtr(true), true},
		{"unknown-model", visionPtr(true), true},
		// Nil resolves from the built-in table.
		{"glm-5.3", nil, true},
		{"claude-sonnet-4-6", nil, true},
		{"gpt-4o-mini", nil, true},
		{"gemini-2.5-flash", nil, true},
		{"qwen2.5-vl-72b", nil, true},
		{"glm-4v-plus", nil, true},
		{"deepseek-v4-pro", nil, false},
		{"glm-4.6", nil, false},
		{"kimi-k3", nil, false},
		{"totally-unknown", nil, false},
		// Provider-prefixed IDs strip the prefix before matching.
		{"zhipu/glm-5.3", nil, true},
		{"deepseek/deepseek-v4-pro", nil, false},
		// o-series text-only variants must not inherit the family vision
		// flag; a false positive would deliver images natively and fail
		// with an API 400.
		{"o1-mini", nil, false},
		{"o1-preview", nil, false},
		{"o3-mini", nil, false},
		{"o1-2024-12-17", nil, true},
		{"o1-pro", nil, true},
		{"o3-pro", nil, true},
		{"o4-mini", nil, true},
	}
	for _, tc := range cases {
		cfg := ModelConfig{Model: tc.model, Vision: tc.vision}
		if got := cfg.EffectiveVision(); got != tc.expected {
			t.Errorf("EffectiveVision(%q, vision=%v) = %v, want %v", tc.model, tc.vision, got, tc.expected)
		}
	}
}

func TestCloneDeepCopiesVisionPointer(t *testing.T) {
	original := Config{Models: []ModelConfig{{Name: "m", Model: "glm-5", Vision: visionPtr(true)}}}
	cloned := original.Clone()
	*cloned.Models[0].Vision = false
	if *original.Models[0].Vision != true {
		t.Fatal("Clone shared the Vision pointer with the original config")
	}
}

func TestVisionRoundTripsThroughJSON(t *testing.T) {
	cfg := Config{Models: []ModelConfig{{Name: "m", Model: "glm-5", Vision: visionPtr(true)}}}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(`"vision":true`)) {
		t.Fatalf("vision field missing from wire format: %s", data)
	}
	var loaded Config
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatal(err)
	}
	if loaded.Models[0].Vision == nil || *loaded.Models[0].Vision != true {
		t.Fatalf("vision did not round-trip: %s", data)
	}
}
