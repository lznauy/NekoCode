package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	if Default.Provider != "deepseek" {
		t.Errorf("expected Provider 'deepseek', got '%s'", Default.Provider)
	}
	if Default.Model != "deepseek-chat" {
		t.Errorf("expected Model 'deepseek-chat', got '%s'", Default.Model)
	}
	if Default.BaseURL != "https://api.deepseek.com/v1" {
		t.Errorf("expected BaseURL 'https://api.deepseek.com/v1', got '%s'", Default.BaseURL)
	}
	if Default.ContextWindow != 128000 {
		t.Errorf("expected ContextWindow 128000, got %d", Default.ContextWindow)
	}
	if Default.FlashModel != "" {
		t.Errorf("expected FlashModel empty, got %s", Default.FlashModel)
	}
}

func TestLoad_NoConfigFile(t *testing.T) {
	// Set HOME to a temp dir with no config file
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if cfg == nil {
		t.Fatal("Load() returned nil config")
	}
	if cfg.Provider != Default.Provider {
		t.Errorf("expected Provider '%s', got '%s'", Default.Provider, cfg.Provider)
	}
	if cfg.Model != Default.Model {
		t.Errorf("expected Model '%s', got '%s'", Default.Model, cfg.Model)
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
		Provider:       "anthropic",
		APIKey:         "sk-test-key",
		Model:          "claude-3-opus",
		BaseURL:        "https://api.anthropic.com",
		ContextWindow:    200000,
		FlashModel: "deepseek-flash",
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

	if cfg.Provider != customCfg.Provider {
		t.Errorf("expected Provider '%s', got '%s'", customCfg.Provider, cfg.Provider)
	}
	if cfg.APIKey != customCfg.APIKey {
		t.Errorf("expected APIKey '%s', got '%s'", customCfg.APIKey, cfg.APIKey)
	}
	if cfg.Model != customCfg.Model {
		t.Errorf("expected Model '%s', got '%s'", customCfg.Model, cfg.Model)
	}
	if cfg.BaseURL != customCfg.BaseURL {
		t.Errorf("expected BaseURL '%s', got '%s'", customCfg.BaseURL, cfg.BaseURL)
	}
	if cfg.ContextWindow != customCfg.ContextWindow {
		t.Errorf("expected ContextWindow %d, got %d", customCfg.ContextWindow, cfg.ContextWindow)
	}
	if cfg.FlashModel != customCfg.FlashModel {
		t.Errorf("expected FlashModel %s, got %s", customCfg.FlashModel, cfg.FlashModel)
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

	// Should fall back to defaults on invalid JSON
	if cfg.Provider != Default.Provider {
		t.Errorf("expected Provider '%s', got '%s'", Default.Provider, cfg.Provider)
	}
}

func TestLoad_PartialConfig(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	configDir := filepath.Join(tmpDir, ".nekocode")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}

	// Only set some fields, others should use defaults
	partialJSON := `{"provider": "custom-provider", "model": "custom-model"}`
	configPath := filepath.Join(configDir, "config.json")
	if err := os.WriteFile(configPath, []byte(partialJSON), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	if cfg.Provider != "custom-provider" {
		t.Errorf("expected Provider 'custom-provider', got '%s'", cfg.Provider)
	}
	if cfg.Model != "custom-model" {
		t.Errorf("expected Model 'custom-model', got '%s'", cfg.Model)
	}
	// These should fall back to defaults
	if cfg.BaseURL != Default.BaseURL {
		t.Errorf("expected BaseURL '%s', got '%s'", Default.BaseURL, cfg.BaseURL)
	}
	if cfg.ContextWindow != Default.ContextWindow {
		t.Errorf("expected ContextWindow %d, got %d", Default.ContextWindow, cfg.ContextWindow)
	}
}

func TestConfig_JSONRoundTrip(t *testing.T) {
	original := Config{
		Provider:       "openai",
		APIKey:         "sk-abc123",
		Model:          "gpt-4-turbo",
		BaseURL:        "https://custom.api.com/v1",
		ContextWindow:    64000,
		FlashModel: "flash",
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var restored Config
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if restored != original {
		t.Errorf("round-trip failed: got %+v, want %+v", restored, original)
	}
}
