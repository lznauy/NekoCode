package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

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

func Path() string {
	return filepath.Join(common.NekocodeHome(), "config.json")
}

func Exists() bool {
	_, err := os.Stat(Path())
	return err == nil
}

type ImageGenConfig struct {
	Name      string `json:"name"`
	Provider  string `json:"provider"`           // e.g. "jimeng"
	APIKey    string `json:"api_key"`            // Volcengine Access Key ID
	SecretKey string `json:"secret_key"`         // Volcengine Secret Access Key
	BaseURL   string `json:"base_url,omitempty"` // default: https://visual.volcengineapi.com
	Model     string `json:"model,omitempty"`    // default: jimeng_t2i_v31
}

type MCPServerConfig struct {
	Command     string            `json:"command"`
	Args        []string          `json:"args,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	DangerLevel string            `json:"dangerLevel,omitempty"`
	Enabled     bool              `json:"enabled"`
}

type Config struct {
	Active         string                     `json:"active"` // name of the active model
	ContextWindow  int                        `json:"context_window"`
	FlashModel     string                     `json:"flash_model,omitempty"` // optional lightweight model; empty uses the active model
	Models         []ModelConfig              `json:"models"`
	ImageGenModels []ImageGenConfig           `json:"image_gen_models,omitempty"` // text-to-image models
	MCPServers     map[string]MCPServerConfig `json:"mcp_servers,omitempty"`
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

	configPath := Path()
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

func Save(cfg Config) error {
	if err := Validate(&cfg); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return common.WriteFileWithDir(Path(), data, 0o600)
}

func Validate(cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("config is nil")
	}
	if cfg.ContextWindow <= 0 {
		cfg.ContextWindow = Default.ContextWindow
	}
	if len(cfg.Models) == 0 {
		return fmt.Errorf("at least one model is required")
	}

	seen := make(map[string]bool, len(cfg.Models))
	for i := range cfg.Models {
		m := &cfg.Models[i]
		m.Name = strings.TrimSpace(m.Name)
		m.Provider = strings.TrimSpace(m.Provider)
		m.APIKey = strings.TrimSpace(m.APIKey)
		m.Model = strings.TrimSpace(m.Model)
		m.BaseURL = strings.TrimSpace(m.BaseURL)
		m.Protocol = strings.TrimSpace(m.Protocol)

		if m.Name == "" {
			return fmt.Errorf("model #%d name is required", i+1)
		}
		if seen[m.Name] {
			return fmt.Errorf("duplicate model name %q", m.Name)
		}
		seen[m.Name] = true
		if m.Provider == "" {
			return fmt.Errorf("model %q provider is required", m.Name)
		}
		if m.Model == "" {
			return fmt.Errorf("model %q model id is required", m.Name)
		}
		if m.Protocol != "" && m.Protocol != "openai" && m.Protocol != "anthropic" {
			return fmt.Errorf("model %q protocol must be openai or anthropic", m.Name)
		}
	}

	cfg.Active = strings.TrimSpace(cfg.Active)
	if cfg.Active == "" {
		cfg.Active = cfg.Models[0].Name
	}
	if !seen[cfg.Active] {
		return fmt.Errorf("active model %q does not exist", cfg.Active)
	}
	cfg.FlashModel = strings.TrimSpace(cfg.FlashModel)
	if cfg.FlashModel != "" && !seen[cfg.FlashModel] {
		return fmt.Errorf("flash model %q does not exist", cfg.FlashModel)
	}

	imageSeen := make(map[string]bool, len(cfg.ImageGenModels))
	for i := range cfg.ImageGenModels {
		m := &cfg.ImageGenModels[i]
		m.Name = strings.TrimSpace(m.Name)
		m.Provider = strings.TrimSpace(m.Provider)
		m.APIKey = strings.TrimSpace(m.APIKey)
		m.SecretKey = strings.TrimSpace(m.SecretKey)
		m.BaseURL = strings.TrimSpace(m.BaseURL)
		m.Model = strings.TrimSpace(m.Model)
		if m.Name == "" {
			return fmt.Errorf("image model #%d name is required", i+1)
		}
		if imageSeen[m.Name] {
			return fmt.Errorf("duplicate image model name %q", m.Name)
		}
		imageSeen[m.Name] = true
		if m.Provider == "" {
			return fmt.Errorf("image model %q provider is required", m.Name)
		}
	}

	if cfg.MCPServers != nil {
		normalized := make(map[string]MCPServerConfig, len(cfg.MCPServers))
		names := make([]string, 0, len(cfg.MCPServers))
		for name := range cfg.MCPServers {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, rawName := range names {
			name := strings.TrimSpace(rawName)
			if name == "" {
				return fmt.Errorf("mcp server name is required")
			}
			if _, exists := normalized[name]; exists {
				return fmt.Errorf("duplicate mcp server name %q", name)
			}
			srv := cfg.MCPServers[rawName]
			srv.Command = strings.TrimSpace(srv.Command)
			srv.DangerLevel = strings.TrimSpace(srv.DangerLevel)
			for i := range srv.Args {
				srv.Args[i] = strings.TrimSpace(srv.Args[i])
			}
			if srv.Env != nil {
				env := make(map[string]string, len(srv.Env))
				for k, v := range srv.Env {
					key := strings.TrimSpace(k)
					if key == "" {
						return fmt.Errorf("mcp server %q has empty env key", name)
					}
					env[key] = strings.TrimSpace(v)
				}
				srv.Env = env
			}
			if srv.Command == "" {
				return fmt.Errorf("mcp server %q command is required", name)
			}
			switch strings.ToLower(srv.DangerLevel) {
			case "", "safe", "modify", "write", "danger", "destructive", "blocked", "forbidden":
			default:
				return fmt.Errorf("mcp server %q dangerLevel must be safe, write, danger, or forbidden", name)
			}
			normalized[name] = srv
		}
		cfg.MCPServers = normalized
	}

	return nil
}

// ResolveModel looks up a named model. Empty names use the active model.
// Unknown names use the active provider settings with the given model name.
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
