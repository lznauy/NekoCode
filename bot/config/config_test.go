package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	if Default.Active != "default" {
		t.Errorf("expected Active 'default', got '%s'", Default.Active)
	}
	if len(Default.Models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(Default.Models))
	}
	m := Default.Models[0]
	if m.Provider != "deepseek" {
		t.Errorf("expected Provider 'deepseek', got '%s'", m.Provider)
	}
	if m.Model != "deepseek-chat" {
		t.Errorf("expected Model 'deepseek-chat', got '%s'", m.Model)
	}
	if m.BaseURL != "https://api.deepseek.com/v1" {
		t.Errorf("expected BaseURL 'https://api.deepseek.com/v1', got '%s'", m.BaseURL)
	}
	if Default.ContextWindow != 128000 {
		t.Errorf("expected ContextWindow 128000, got %d", Default.ContextWindow)
	}
	if Default.FlashModel != "" {
		t.Errorf("expected FlashModel empty, got %s", Default.FlashModel)
	}
}

func TestLoad_NoConfigFile(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if cfg == nil {
		t.Fatal("Load() returned nil config")
	}
	if cfg.Active != Default.Active {
		t.Errorf("expected Active '%s', got '%s'", Default.Active, cfg.Active)
	}
}

func TestLoad_ValidConfigFile(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	configDir := filepath.Join(tmpDir, ".nekocode")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}

	customCfg := Config{
		Active:        "claude",
		ContextWindow: 200000,
		FlashModel:    "deepseek-flash",
		Models: []ModelConfig{
			{Name: "default", Provider: "deepseek", APIKey: "sk-ds", Model: "deepseek-chat", BaseURL: "https://api.deepseek.com/v1"},
			{Name: "claude", Provider: "anthropic", APIKey: "sk-ant", Model: "claude-3-opus", BaseURL: "https://api.anthropic.com", Protocol: "anthropic"},
		},
	}

	data, err := json.Marshal(customCfg)
	if err != nil {
		t.Fatalf("failed to marshal config: %v", err)
	}

	configPath := filepath.Join(configDir, "config.json")
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if cfg == nil {
		t.Fatal("Load() returned nil config")
	}

	if cfg.Active != customCfg.Active {
		t.Errorf("expected Active '%s', got '%s'", customCfg.Active, cfg.Active)
	}
	if cfg.ContextWindow != customCfg.ContextWindow {
		t.Errorf("expected ContextWindow %d, got %d", customCfg.ContextWindow, cfg.ContextWindow)
	}
	if cfg.FlashModel != customCfg.FlashModel {
		t.Errorf("expected FlashModel %s, got %s", customCfg.FlashModel, cfg.FlashModel)
	}
	if len(cfg.Models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(cfg.Models))
	}

	am := cfg.ActiveModelConfig()
	if am.Name != "claude" || am.Provider != "anthropic" || am.Model != "claude-3-opus" {
		t.Errorf("unexpected active config: %+v", am)
	}
}

func TestLoad_InvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	configDir := filepath.Join(tmpDir, ".nekocode")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}

	configPath := filepath.Join(configDir, "config.json")
	if err := os.WriteFile(configPath, []byte("not valid json {{{"), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if cfg == nil {
		t.Fatal("Load() returned nil config")
	}

	if cfg.Active != Default.Active {
		t.Errorf("expected Active '%s', got '%s'", Default.Active, cfg.Active)
	}
}

func TestLoad_PartialConfig(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	configDir := filepath.Join(tmpDir, ".nekocode")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}

	partialJSON := `{"active": "gpt4", "models": [{"name": "gpt4", "provider": "openai", "api_key": "sk-xxx", "model": "gpt-4-turbo"}]}`
	configPath := filepath.Join(configDir, "config.json")
	if err := os.WriteFile(configPath, []byte(partialJSON), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	if cfg.Active != "gpt4" {
		t.Errorf("expected Active 'gpt4', got '%s'", cfg.Active)
	}
	if cfg.ContextWindow != Default.ContextWindow {
		t.Errorf("expected ContextWindow %d, got %d", Default.ContextWindow, cfg.ContextWindow)
	}
}

func TestConfig_JSONRoundTrip(t *testing.T) {
	original := Config{
		Active:        "default",
		ContextWindow: 64000,
		FlashModel:    "flash",
		Models: []ModelConfig{
			{Name: "default", Provider: "openai", APIKey: "sk-abc", Model: "gpt-4-turbo", BaseURL: "https://custom.api.com/v1"},
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var restored Config
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if restored.Active != original.Active || restored.ContextWindow != original.ContextWindow || restored.FlashModel != original.FlashModel {
		t.Errorf("round-trip failed: got %+v, want %+v", restored, original)
	}
	if len(restored.Models) != 1 || restored.Models[0].Name != "default" {
		t.Errorf("models round-trip failed")
	}
}

func TestConfig_ModelsList(t *testing.T) {
	cfg := Config{
		Active: "default",
		Models: []ModelConfig{
			{Name: "default", Provider: "deepseek", APIKey: "sk-ds", Model: "deepseek-chat", BaseURL: "https://api.deepseek.com/v1"},
			{Name: "claude", Provider: "anthropic", APIKey: "sk-ant", Model: "claude-sonnet-4-6", BaseURL: "https://api.anthropic.com", Protocol: "anthropic"},
			{Name: "gpt4", Provider: "openai", APIKey: "sk-openai", Model: "gpt-4-turbo"},
		},
	}

	names := cfg.AllModelNames()
	if len(names) != 3 {
		t.Fatalf("expected 3 model names, got %d: %v", len(names), names)
	}
	if names[0] != "default" || names[1] != "claude" || names[2] != "gpt4" {
		t.Errorf("unexpected names: %v", names)
	}

	am := cfg.ActiveModelConfig()
	if am.Name != "default" || am.Provider != "deepseek" || am.Model != "deepseek-chat" {
		t.Errorf("unexpected active config: %+v", am)
	}

	if !cfg.SwitchModel("claude") {
		t.Fatal("SwitchModel(claude) failed")
	}
	if cfg.Active != "claude" {
		t.Errorf("expected Active 'claude', got '%s'", cfg.Active)
	}

	am = cfg.ActiveModelConfig()
	if am.Name != "claude" || am.Provider != "anthropic" || am.APIKey != "sk-ant" || am.Model != "claude-sonnet-4-6" {
		t.Errorf("SwitchModel(claude) active config wrong: %+v", am)
	}

	if !cfg.SwitchModel("gpt4") {
		t.Fatal("SwitchModel(gpt4) failed")
	}
	am = cfg.ActiveModelConfig()
	if am.Provider != "openai" || am.Model != "gpt-4-turbo" || am.APIKey != "sk-openai" {
		t.Errorf("SwitchModel(gpt4) active config wrong: %+v", am)
	}

	if cfg.SwitchModel("unknown") {
		t.Error("SwitchModel(unknown) should return false")
	}
}

func TestConfig_ModelsJSONRoundTrip(t *testing.T) {
	original := Config{
		Active: "claude",
		Models: []ModelConfig{
			{Name: "default", Provider: "deepseek", APIKey: "sk-ds", Model: "deepseek-chat", BaseURL: "https://api.deepseek.com/v1"},
			{Name: "claude", Provider: "anthropic", APIKey: "sk-ant", Model: "claude-sonnet-4-6", Protocol: "anthropic"},
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var restored Config
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if len(restored.Models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(restored.Models))
	}
	if restored.Active != "claude" {
		t.Errorf("expected active 'claude', got %s", restored.Active)
	}
}
