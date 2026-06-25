package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"nekocode/common"
)

type ModelConfig struct {
	Name     string `json:"name"`
	Provider string `json:"provider"`
	APIKey   string `json:"api_key"`
	Model    string `json:"model"`
	BaseURL  string `json:"base_url,omitempty"`
	Protocol string `json:"protocol,omitempty"`
}

type ImageGenConfig struct {
	Name      string `json:"name"`
	Provider  string `json:"provider"`           // e.g. "jimeng"
	APIKey    string `json:"api_key"`            // Volcengine Access Key ID
	SecretKey string `json:"secret_key"`         // Volcengine Secret Access Key
	BaseURL   string `json:"base_url,omitempty"` // default: https://visual.volcengineapi.com
	Model     string `json:"model,omitempty"`    // default: jimeng_t2i_v31
}

type Config struct {
	Active         string           `json:"active"` // name of the active model
	ContextWindow  int              `json:"context_window"`
	FlashModel     string           `json:"flash_model,omitempty"` // cheap model for sub-tasks (subagents)
	Models         []ModelConfig    `json:"models"`
	ImageGenModels []ImageGenConfig `json:"image_gen_models,omitempty"` // text-to-image models
}

var Default = Config{
	Active:        "default",
	ContextWindow: 128000,
	Models: []ModelConfig{
		{
			Name:     "default",
			Provider: "deepseek",
			Model:    "deepseek-chat",
			BaseURL:  "https://api.deepseek.com/v1",
		},
	},
	ImageGenModels: []ImageGenConfig{
		{
			Name:     "jimeng",
			Provider: "jimeng",
			Model:    "jimeng_t2i_v31",
			BaseURL:  "https://visual.volcengineapi.com",
		},
	},
}

func Load() (*Config, error) {
	// Always copy Default so callers that mutate the config (e.g. SwitchModel)
	// cannot pollute the package-level global.
	cfg := Default

	configPath := filepath.Join(common.NekocodeHome(), "config.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return &cfg, nil
	}

	if err := json.Unmarshal(data, &cfg); err != nil {
		fmt.Fprintf(os.Stderr, "config: %s is malformed JSON (%v) — using defaults. Fix or delete the file to silence this warning.\n", configPath, err)
		return &cfg, nil
	}

	// Validate Active points to an existing model.
	if cfg.Active != "" && len(cfg.Models) > 0 {
		found := false
		for _, m := range cfg.Models {
			if m.Name == cfg.Active {
				found = true
				break
			}
		}
		if !found {
			fmt.Fprintf(os.Stderr, "config: active model %q not found, falling back to %q\n", cfg.Active, cfg.Models[0].Name)
			cfg.Active = cfg.Models[0].Name
		}
	}

	return &cfg, nil
}

// ResolveModel looks up a named model. If found, returns its full config.
// If not found, falls back to the active model's config with the given name as the model field.
func (c *Config) ResolveModel(name string) ModelConfig {
	if name == "" {
		return c.ActiveModelConfig()
	}
	if fm, ok := c.LookupModelConfig(name); ok {
		return fm
	}
	am := c.ActiveModelConfig()
	am.Model = name
	return am
}

// ActiveModelConfig returns the ModelConfig for the currently active model.
func (c *Config) ActiveModelConfig() ModelConfig {
	for _, m := range c.Models {
		if m.Name == c.Active {
			return m
		}
	}
	// Fallback to first model if active not found
	if len(c.Models) > 0 {
		return c.Models[0]
	}
	return ModelConfig{}
}

// LookupModelConfig returns the ModelConfig for a named model.
func (c *Config) LookupModelConfig(name string) (ModelConfig, bool) {
	for _, m := range c.Models {
		if m.Name == name {
			return m, true
		}
	}
	return ModelConfig{}, false
}

// AllModelNames returns all available model names.
func (c *Config) AllModelNames() []string {
	names := make([]string, 0, len(c.Models))
	for _, m := range c.Models {
		names = append(names, m.Name)
	}
	return names
}

// SwitchModel switches to the named model. Returns false if not found.
func (c *Config) SwitchModel(name string) bool {
	for _, m := range c.Models {
		if m.Name == name {
			c.Active = name
			return true
		}
	}
	return false
}
