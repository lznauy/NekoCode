package viewmodel

import (
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
		Active:             "main",
		AutoCompactPercent: 75,
		Models: []config.ModelConfig{{
			Name: "main", Provider: "openai", Model: "gpt-5", ReasoningEffort: "medium",
		}},
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
	if got := ToConfig(view).Models[0].ReasoningEffort; got != "medium" {
		t.Fatalf("round-trip effort = %q", got)
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

func TestSessionSnapshot(t *testing.T) {
	got := SessionSnapshot(&session.Snapshot{
		ID: "s1", CWD: "/work", CreatedAt: 1, UpdatedAt: 2,
		Messages: []types.Message{{Role: "user", Content: "hello"}},
	})
	if got.ID != "s1" || got.CWD != "/work" || got.MsgCount != 1 {
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
