package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestKnownContextWindow(t *testing.T) {
	cases := map[string]int{
		// Current generation (verified 2026-08)
		"deepseek-v4-pro":      1048576,
		"deepseek-v4-flash":    1048576,
		"deepseek/deepseek-v4": 1048576, // proxy-prefixed ID
		"claude-opus-4-8":      1048576,
		"claude-sonnet-5":      1048576,
		"claude-fable-5":       1048576,
		"claude-haiku-4-5":     200000,
		"gpt-5.2":              400000,
		"gpt-5.4":              1050000,
		"gpt-4.1":              1047576,
		"gpt-4o":               128000,
		"glm-5.3":              1048576,
		"glm-5.3-flash":        1048576,
		"glm-5.2":              1048576,
		"glm-4.6":              200000,
		"glm-4.5-air":          131072,
		"kimi-k3":              1048576,
		"kimi-k2.6":            262144,
		"grok-4.5":             512000,
		"grok-4.1":             262144,
		"llama-4-scout":        10485760,
		"llama-4-maverick":     1048576,
		"gemini-2.5-pro":       1048576,
		"qwen3-235b-a22b":      131072,
		"moonshot-v1-128k":     131072,
		// Retired aliases of DeepSeek V4-Flash
		"deepseek-chat":     1048576,
		"DeepSeek-Reasoner": 1048576, // case-insensitive
		// Older Claude generations fall back to 200K
		"claude-sonnet-4-5": 200000,
		"claude-3-5-sonnet": 200000,
	}
	for model, want := range cases {
		got, ok := KnownContextWindow(model)
		if !ok || got != want {
			t.Errorf("KnownContextWindow(%q) = %d, %v; want %d", model, got, ok, want)
		}
	}
	if _, ok := KnownContextWindow("my-finetune-v9"); ok {
		t.Error("unknown model should not hit the table")
	}
	if _, ok := KnownContextWindow("proxy-my-gpt-5.4-wrapper"); ok {
		t.Error("embedded model-family text should not produce a false profile match")
	}
}

// writeConfig writes data as the user config file under a temp HOME.
func writeConfig(t *testing.T, data string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), ".nekocode")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", filepath.Dir(dir))
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLoadAutoResolvesContextWindow(t *testing.T) {
	// No per-model override: resolve from the active model's name.
	writeConfig(t, `{
		"active": "ds",
		"models": [{"name": "ds", "provider": "deepseek", "model": "deepseek-v4-flash", "api_key": "k"}]
	}`)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.EffectiveContextWindow(); got != 1048576 {
		t.Fatalf("effective window = %d, want 1048576 (deepseek-v4-flash)", got)
	}
}

func TestLoadPerModelWindowWinsOverTable(t *testing.T) {
	writeConfig(t, `{
		"active": "ds",
		"models": [{"name": "ds", "provider": "deepseek", "model": "deepseek-v4-flash", "api_key": "k", "context_window": 96000}]
	}`)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.EffectiveContextWindow(); got != 96000 {
		t.Fatalf("effective window = %d, want 96000 (per-model override)", got)
	}
}

func TestLoadUnknownModelFallsBackToDefault(t *testing.T) {
	writeConfig(t, `{
		"active": "x",
		"models": [{"name": "x", "provider": "custom", "model": "my-finetune", "api_key": "k"}]
	}`)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.EffectiveContextWindow(); got != DefaultContextWindow {
		t.Fatalf("effective window = %d, want default %d", got, DefaultContextWindow)
	}
}

func TestSwitchModelReResolvesWindow(t *testing.T) {
	writeConfig(t, `{
		"active": "ds",
		"models": [
			{"name": "ds", "provider": "deepseek", "model": "deepseek-v4-flash", "api_key": "k"},
			{"name": "claude", "provider": "anthropic", "model": "claude-haiku-4-5", "api_key": "k"}
		]
	}`)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.EffectiveContextWindow(); got != 1048576 {
		t.Fatalf("initial window = %d, want 1048576", got)
	}
	if !cfg.SwitchModel("claude") {
		t.Fatal("SwitchModel failed")
	}
	if got := cfg.EffectiveContextWindow(); got != 200000 {
		t.Fatalf("after switch window = %d, want 200000 (claude-haiku)", got)
	}
}

func TestSaveOmitsUnsetGlobalWindow(t *testing.T) {
	// A config that never pinned a window must not get one written back —
	// otherwise auto-resolution would freeze into an explicit value.
	writeConfig(t, `{
		"active": "ds",
		"models": [{"name": "ds", "provider": "deepseek", "model": "deepseek-v4-flash", "api_key": "k"}]
	}`)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := Save(*cfg); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(os.Getenv("HOME"), ".nekocode", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var probe map[string]any
	if err := json.Unmarshal(data, &probe); err != nil {
		t.Fatal(err)
	}
	if _, present := probe["context_window"]; present {
		t.Fatalf("unset window must stay out of the file: %s", data)
	}
}
