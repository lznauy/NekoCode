package viewmodel

import (
	"encoding/json"
	"testing"

	"nekocode/bot/config"
	"nekocode/bot/provider/types"
	"nekocode/bot/session"
	controlruntime "nekocode/runtime"
)

func TestModel(t *testing.T) {
	got := Model(config.ModelConfig{Provider: "openai", Model: "gpt", ReasoningEffort: "high"})
	if got.Provider != "openai" || got.Model != "gpt" || got.ReasoningEffort != "high" {
		t.Fatalf("model view = %+v", got)
	}
}

func TestConfigPreservesReasoningEffort(t *testing.T) {
	source := config.Config{
		Active:             "openai/main",
		AutoCompactPercent: 75,
		Providers:          []config.ProviderConfig{{Name: "openai", Models: []config.ModelDefinition{{Name: "main", Model: "gpt-5", ReasoningEffort: "medium"}}}},
	}
	view := Config(source)
	if view.AutoCompactPercent != 75 || ToConfig(view).AutoCompactPercent != 75 {
		t.Fatalf("auto compaction threshold did not round-trip: %+v", view)
	}
	if got := view.Models[0].ReasoningEffort; got != "medium" {
		t.Fatalf("config view effort = %q", got)
	}
	if got := view.Models[0].Profile.ReasoningEfforts; len(got) != 4 || got[0] != "minimal" {
		t.Fatalf("config view supported efforts = %v", got)
	}
	if got := ToConfig(view).AllModels()[0].ReasoningEffort; got != "medium" {
		t.Fatalf("round-trip effort = %q", got)
	}
}

func TestProviderViewRoundTripPreservesSourceAndIgnoresCatalogEdits(t *testing.T) {
	var source config.Config
	if err := json.Unmarshal([]byte(`{"auto_compact_percent":80,"providers":[{"name":"relay","api_key":"key","models":["model-a",{"name":"coder","model":"gpt-5","vision":false}]}],"active":"relay/coder","compaction_model":"relay/model-a"}`), &source); err != nil {
		t.Fatal(err)
	}
	before, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	view := Config(source)
	view.Models[0].APIKey = "catalog-must-not-write-credentials"
	view.Models[1].Vision = nil
	restored := ToConfig(view)
	after, err := json.Marshal(restored)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("config view changed source structure:\nbefore=%s\nafter=%s", before, after)
	}
	*view.Providers[0].Models[1].Vision = true
	if *source.Providers[0].Models[1].Vision || *restored.Providers[0].Models[1].Vision {
		t.Fatal("view shares model state with configuration")
	}
}

func TestConfigPreservesImageUnderstandModels(t *testing.T) {
	source := config.Config{
		ImageUnderstandModels: []config.ImageUnderstandConfig{{
			Name: "vision", Provider: "anthropic", APIKey: "secret", Model: "claude", BaseURL: "https://example.com", Protocol: "anthropic",
		}},
	}
	view := Config(source)
	if len(view.ImageUnderstandModels) != 1 || view.ImageUnderstandModels[0].Model != "claude" {
		t.Fatalf("unexpected image understand view: %+v", view.ImageUnderstandModels)
	}
	got := ToConfig(view).ImageUnderstandModels
	if len(got) != 1 || got[0] != source.ImageUnderstandModels[0] {
		t.Fatalf("image understand config did not round-trip: %+v", got)
	}
}

func TestModelProfileResolvesOverrideKnownAndDefaultWindows(t *testing.T) {
	tests := []struct {
		model  config.ModelConfig
		window int
		source string
	}{
		{config.ModelConfig{Model: "gpt-5", ContextWindow: 64_000}, 64_000, "override"},
		{config.ModelConfig{Model: "deepseek-v4-flash"}, 1_048_576, "model"},
		{config.ModelConfig{Model: "custom-model"}, config.DefaultContextWindow, "default"},
	}
	for _, test := range tests {
		profile := ModelProfile(test.model)
		if profile.ContextWindow != test.window || profile.ContextWindowSource != test.source {
			t.Errorf("ModelProfile(%+v) = %+v", test.model, profile)
		}
	}
}

func TestSessionMeta(t *testing.T) {
	got := SessionMetas([]session.Meta{{
		ID: "s1", ForkedFrom: "source", ForkSourceDeleted: true,
	}})
	if len(got) != 1 || got[0].ForkedFrom != "source" || !got[0].ForkSourceDeleted {
		t.Fatalf("session metadata view = %+v", got)
	}
}

func TestSessionSnapshot(t *testing.T) {
	got := SessionSnapshot(&session.Snapshot{
		ID: "s1", CWD: "/work", CreatedAt: 1, UpdatedAt: 2, ForkedFrom: "source",
		Messages: []types.Message{{Role: "user", Content: "hello"}},
	})
	if got.ID != "s1" || got.CWD != "/work" || got.MsgCount != 1 || got.ForkedFrom != "source" {
		t.Fatalf("session view = %+v", got)
	}
	if got := SessionSnapshot(nil); got != (controlruntime.SessionMeta{}) {
		t.Fatalf("nil session view = %+v", got)
	}
}

func TestMemory(t *testing.T) {
	got := Memory("", "/memory.md", `[Project Memory]
## Tech Stack
- Go

## Active Goals
- converge runtime

## User Preferences
- concise replies`)
	if got.Scope != controlruntime.MemoryScopeProject || got.Path != "/memory.md" || got.Empty {
		t.Fatalf("memory view = %+v", got)
	}
	if len(got.Sections) != 5 {
		t.Fatalf("memory sections = %d, want 5", len(got.Sections))
	}
	if got.Sections[0].Content != "- Go" || got.Sections[1].Content != "- converge runtime" {
		t.Fatalf("parsed memory sections = %+v", got.Sections)
	}
	if !got.Sections[2].Empty || got.Sections[4].Content != "- concise replies" {
		t.Fatalf("optional memory sections = %+v", got.Sections)
	}
}
