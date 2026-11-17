package config

import (
	"encoding/json"
	"nekocode/bot/modelcatalog"
	"nekocode/bot/reasoning"
	"reflect"
	"testing"
)

func reasoningConfig(t *testing.T, id string, supported bool, options any) Config {
	t.Helper()
	entry := map[string]any{"id": id, "modalities": map[string]any{"output": []string{"text"}}, "limit": map[string]any{"context": 96000, "output": 16000}, "reasoning": supported}
	if options != nil {
		entry["reasoning_options"] = options
	}
	raw, _ := json.Marshal(map[string]any{"p": map[string]any{"api": "https://service.example/v1", "models": map[string]any{id: entry}}})
	catalog, err := modelcatalog.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	return Config{Catalog: catalog, Providers: []ProviderConfig{{Name: "p", BaseURL: "https://service.example/v1", Models: []ModelDefinition{{Model: id}}}}}
}
func effortOptions(values ...string) any {
	return []any{map[string]any{"type": "effort", "values": values}}
}

func TestPublicEffortsDriveValidationAndResolution(t *testing.T) {
	cfg := reasoningConfig(t, "brand-new", true, effortOptions("none", "low", "high", "max"))
	model := cfg.ActiveModelConfig()
	if got := ReasoningCapabilityFor(model).Values(); !reflect.DeepEqual(got, []string{"auto", "none", "low", "high", "max"}) {
		t.Fatal(got)
	}
	cfg.Providers[0].Models[0].ReasoningEffort = "high"
	if err := Validate(&cfg); err != nil {
		t.Fatalf("validation ignored fixed catalog: %v", err)
	}
	settings, ok := ResolveReasoning(cfg.ActiveModelConfig())
	if !ok || settings.Effort != "high" || settings.ThinkingMode != "" {
		t.Fatalf("wrong settings: %+v %v", settings, ok)
	}
	cfg.Providers[0].Models[0].ReasoningEffort = "none"
	settings, ok = ResolveReasoning(cfg.ActiveModelConfig())
	if !ok || !settings.Disabled || settings.DisableEffort != "none" {
		t.Fatalf("none lost: %+v", settings)
	}
	cfg.Providers[0].Models[0].ReasoningEffort = "medium"
	if err := Validate(&cfg); err == nil {
		t.Fatal("unsupported effort accepted")
	}
}
func TestCatalogReasoningPreservesLocalProtocolAndReplay(t *testing.T) {
	cfg := reasoningConfig(t, "deepseek-v4-flash", true, []any{map[string]any{"type": "toggle"}, map[string]any{"type": "effort", "values": []string{"low", "high"}}})
	model := cfg.ActiveModelConfig()
	capability := ReasoningCapabilityFor(model)
	if !reflect.DeepEqual(capability.Values(), []string{"auto", "none", "low", "high"}) || capability.ThinkingMode != "enabled" || capability.Replay != reasoning.ReplayToolCalls {
		t.Fatalf("local protocol lost: %+v", capability)
	}
	cfg = reasoningConfig(t, "brand-new", true, []any{map[string]any{"type": "toggle"}})
	if got := ReasoningCapabilityFor(cfg.ActiveModelConfig()).Values(); !reflect.DeepEqual(got, []string{"auto"}) {
		t.Fatalf("toggle invented effort: %v", got)
	}
	cfg = reasoningConfig(t, "brand-new", true, []any{map[string]any{"type": "budget_tokens", "min": 1024}})
	if got := ReasoningCapabilityFor(cfg.ActiveModelConfig()).Values(); !reflect.DeepEqual(got, []string{"auto"}) {
		t.Fatalf("budget invented effort: %v", got)
	}
}
func TestMissingReasoningFallsBackButExplicitFalseDoesNot(t *testing.T) {
	cfg := reasoningConfig(t, "deepseek-v4-flash", true, nil)
	if len(ReasoningCapabilityFor(cfg.ActiveModelConfig()).Efforts) == 0 {
		t.Fatal("missing data discarded local fallback")
	}
	cfg = reasoningConfig(t, "deepseek-v4-flash", false, nil)
	if got := ReasoningCapabilityFor(cfg.ActiveModelConfig()).Values(); !reflect.DeepEqual(got, []string{"auto"}) {
		t.Fatalf("explicit false ignored: %v", got)
	}
	cfg = reasoningConfig(t, "brand-new", true, effortOptions("high"))
	model := cfg.ActiveModelConfig()
	model.BaseURL = "https://unrelated.example/v1"
	if len(ReasoningCapabilityFor(model).Efforts) != 0 {
		t.Fatal("capability leaked across service endpoints")
	}
}

func TestOriginEffortFallbackDoesNotChangeContextOrServicePrecedence(t *testing.T) {
	raw := `{"author":{"api":"https://official.example/v1","models":{"new-exact":{"id":"new-exact","canonical_model_id":"author/new-exact","modalities":{"output":["text"]},"limit":{"context":96000},"reasoning":true,"reasoning_options":[{"type":"effort","values":["low","high"]}]}}},"relay":{"api":"https://known-relay.example/v1","models":{"new-exact":{"id":"new-exact","modalities":{"output":["text"]},"limit":{"context":64000},"reasoning":false}}}}`
	catalog, err := modelcatalog.Decode([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	cfg := Config{Catalog: catalog, Providers: []ProviderConfig{{Name: "custom", BaseURL: "https://unknown-relay.example/v1", Models: []ModelDefinition{{Model: "new-exact"}}}}}
	model := cfg.ActiveModelConfig()
	cap := ReasoningCapabilityFor(model)
	if !reflect.DeepEqual(cap.Values(), []string{"auto", "low", "high"}) || cap.Source != "origin" || cap.Reference != "author/new-exact" || cap.ReferenceNotice() == "" {
		t.Fatalf("origin fallback missing: %+v", cap)
	}
	if model.EffectiveContextWindow() != DefaultContextWindow {
		t.Fatal("origin changed context budget")
	}
	cfg.Providers[0].BaseURL = "https://known-relay.example/v1"
	cap = ReasoningCapabilityFor(cfg.ActiveModelConfig())
	if cap.Source != "catalog" || !reflect.DeepEqual(cap.Values(), []string{"auto"}) {
		t.Fatalf("origin overrode explicit service denial: %+v", cap)
	}
	cfg.Providers[0].Models[0].Model = "newexact"
	if ReasoningCapabilityFor(cfg.ActiveModelConfig()).Source == "origin" {
		t.Fatal("fuzzy matching was introduced")
	}
}

func TestTencentGLMFlashUsesOriginEfforts(t *testing.T) {
	model := ModelConfig{Model: "glm-5.3-flash", BaseURL: "https://copilot.tencent.com/v2/chat/completions", Protocol: "openai"}
	cap := ReasoningCapabilityFor(model)
	if !reflect.DeepEqual(cap.Values(), []string{"auto", "low", "high", "max"}) || cap.Source != "origin" || cap.Reference != "zhipuai/glm-5.3-flash" {
		t.Fatalf("actual relay case still unresolved: %+v", cap)
	}
	if cap.ThinkingMode != "enabled" {
		t.Fatal("lost local GLM wire adapter")
	}
}
