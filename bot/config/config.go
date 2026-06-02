package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type Config struct {
	Provider       string `json:"provider"`
	APIKey         string `json:"api_key"`
	Model          string `json:"model"`
	BaseURL        string `json:"base_url"`
	Protocol       string `json:"protocol,omitempty"` // "openai" (default) or "anthropic"
	ContextWindow int    `json:"context_window"`
	FlashModel    string `json:"flash_model,omitempty"` // cheap model for sub-tasks (subagents)
}

var Default = Config{
	Provider:       "deepseek",
	Model:          "deepseek-chat",
	BaseURL:        "https://api.deepseek.com/v1",
	ContextWindow: 128000,
}

func Load() (*Config, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return &Default, nil
	}

	configPath := filepath.Join(homeDir, ".nekocode", "config.json")

	data, err := os.ReadFile(configPath)
	if err != nil {
		return &Default, nil
	}

	cfg := Default
	if err := json.Unmarshal(data, &cfg); err != nil {
		return &Default, nil
	}

	return &cfg, nil
}
