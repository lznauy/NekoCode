package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"nekocode/util/fs"
	utilhttp "nekocode/util/http"
)

func Path() string {
	return filepath.Join(fs.NekocodeHome(), "config.json")
}

func Exists() bool {
	_, err := os.Stat(Path())
	return err == nil
}

type ModelConfig struct {
	Name            string `json:"name"`
	Provider        string `json:"provider"`
	APIKey          string `json:"api_key"`
	Model           string `json:"model"`
	BaseURL         string `json:"base_url,omitempty"`
	Protocol        string `json:"protocol,omitempty"`
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
	// ContextWindow overrides the context window for this model. When 0,
	// the window is resolved from the built-in model table, then Default.
	ContextWindow int `json:"context_window,omitempty"`
}

type ImageGenConfig struct {
	Name      string `json:"name"`
	Provider  string `json:"provider"`
	APIKey    string `json:"api_key"`
	SecretKey string `json:"secret_key"`
	BaseURL   string `json:"base_url,omitempty"`
	Model     string `json:"model,omitempty"`
}

type MCPServerConfig struct {
	URL                    string            `json:"url,omitempty"`
	Headers                map[string]string `json:"headers,omitempty"`
	OAuthClientID          string            `json:"oauth_client_id,omitempty"`
	OAuthClientSecret      string            `json:"oauth_client_secret,omitempty"`
	OAuthClientMetadataURL string            `json:"oauth_client_metadata_url,omitempty"`
	OAuthCallbackPort      int               `json:"oauth_callback_port,omitempty"`

	Command string            `json:"command"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	Enabled bool              `json:"enabled"`
}

type PermissionsConfig struct {
	Allow   []string                 `json:"allow,omitempty"`
	Ask     []string                 `json:"ask,omitempty"`
	Deny    []string                 `json:"deny,omitempty"`
	Sandbox map[string]SandboxConfig `json:"sandbox,omitempty"`
}

type SandboxConfig struct {
	SandboxMode   string   `json:"sandbox_mode,omitempty"`
	Network       bool     `json:"network,omitempty"`
	WritableRoots []string `json:"writable_roots,omitempty"`
}

type WorkspaceConfig struct {
	Path   string `json:"path"`
	Access string `json:"access,omitempty"`
}

type Config struct {
	Active             string                     `json:"active"`                // name of the active model
	FlashModel         string                     `json:"flash_model,omitempty"` // optional lightweight model; empty uses the active model
	AutoCompactPercent int                        `json:"auto_compact_percent,omitempty"`
	Models             []ModelConfig              `json:"models"`
	ImageGenModels     []ImageGenConfig           `json:"image_gen_models,omitempty"` // text-to-image models
	MCPServers         map[string]MCPServerConfig `json:"mcp_servers,omitempty"`
	Permissions        *PermissionsConfig         `json:"permissions,omitempty"`
	Workspaces         []WorkspaceConfig          `json:"workspaces,omitempty"`
	Jev                *JevConfig                 `json:"jev,omitempty"` // optional "System One" decision engine (e.g. context-compaction relevance pruning)
}

// JevConfig enables the optional Jev decision engine. Only api_key (or the
// TYPESAFE_API_KEY environment variable) is required — every other field
// carries a sensible default, so a minimal config is three lines:
//
//	"jev": { "api_key": "..." }
type JevConfig struct {
	APIKey        string   `json:"api_key,omitempty"`        // falls back to the TYPESAFE_API_KEY environment variable
	Model         string   `json:"model,omitempty"`          // default jev-latest; pin jev-1.x for stable thresholds
	BaseURL       string   `json:"base_url,omitempty"`       // default https://api.typesafe.ai/v1/systemone
	KeepThreshold *float64 `json:"keep_threshold,omitempty"` // noul below this prunes a tool result; default 0.2, explicit 0 disables pruning
	Enabled       *bool    `json:"enabled,omitempty"`        // master switch; nil/true enables the engine, explicit false disables it even when a key is present
}

// DefaultContextWindow is the final fallback context window for models the
// built-in table does not know and no config value overrides.
const DefaultContextWindow = 128000

// DefaultAutoCompactPercent starts full-summary compaction before the model's
// context window is exhausted, leaving room for the summary request and output.
const DefaultAutoCompactPercent = 80

var Default = Config{
	Active:             "default",
	AutoCompactPercent: DefaultAutoCompactPercent,
	Models: []ModelConfig{
		{
			Name:     "default",
			Provider: "deepseek",
			Model:    "deepseek-v4-flash",
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
	cfg := Default.Clone()

	configPath := Path()
	data, err := os.ReadFile(configPath)
	if err != nil {
		return &cfg, nil
	}
	_ = os.Chmod(configPath, 0o600)

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
	for i := range cfg.Models {
		model := &cfg.Models[i]
		model.ReasoningEffort = strings.ToLower(strings.TrimSpace(model.ReasoningEffort))
		if !ReasoningCapabilityFor(*model).Supports(model.ReasoningEffort) {
			fmt.Fprintf(os.Stderr, "config: model %q does not support reasoning_effort %q; using auto\n", model.Name, model.ReasoningEffort)
			model.ReasoningEffort = ReasoningAuto
		}
	}

	return &cfg, nil
}

// EffectiveContextWindow resolves the context window for the active model:
// the per-model override wins, then the built-in model table, and finally
// DefaultContextWindow (128000). The window is a model property, so it is
// resolved on read rather than stored.
func (c *Config) EffectiveContextWindow() int {
	m := c.ActiveModelConfig()
	if m.ContextWindow > 0 {
		return m.ContextWindow
	}
	if w, ok := KnownContextWindow(m.Model); ok {
		return w
	}
	return DefaultContextWindow
}

func (c *Config) EffectiveAutoCompactPercent() int {
	if c.AutoCompactPercent >= 1 && c.AutoCompactPercent <= 99 {
		return c.AutoCompactPercent
	}
	return DefaultAutoCompactPercent
}

// Clone returns an independently mutable configuration value.
func (c Config) Clone() Config {
	out := c
	out.Models = append([]ModelConfig(nil), c.Models...)
	out.ImageGenModels = append([]ImageGenConfig(nil), c.ImageGenModels...)
	out.Workspaces = append([]WorkspaceConfig(nil), c.Workspaces...)

	if c.MCPServers != nil {
		out.MCPServers = make(map[string]MCPServerConfig, len(c.MCPServers))
		for name, server := range c.MCPServers {
			server.Args = append([]string(nil), server.Args...)
			server.Env = cloneStrings(server.Env)
			out.MCPServers[name] = server
		}
	}
	if c.Permissions != nil {
		permissions := *c.Permissions
		permissions.Allow = append([]string(nil), c.Permissions.Allow...)
		permissions.Ask = append([]string(nil), c.Permissions.Ask...)
		permissions.Deny = append([]string(nil), c.Permissions.Deny...)
		if c.Permissions.Sandbox != nil {
			permissions.Sandbox = make(map[string]SandboxConfig, len(c.Permissions.Sandbox))
			for name, sandbox := range c.Permissions.Sandbox {
				sandbox.WritableRoots = append([]string(nil), sandbox.WritableRoots...)
				permissions.Sandbox[name] = sandbox
			}
		}
		out.Permissions = &permissions
	}
	if c.Jev != nil {
		jev := *c.Jev
		out.Jev = &jev
	}
	return out
}

func cloneStrings(input map[string]string) map[string]string {
	if input == nil {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
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
	return fs.WriteFileWithDir(Path(), data, 0o600)
}

func Validate(cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("config is nil")
	}
	if len(cfg.Models) == 0 {
		return fmt.Errorf("at least one model is required")
	}
	if cfg.AutoCompactPercent == 0 {
		cfg.AutoCompactPercent = DefaultAutoCompactPercent
	}
	if cfg.AutoCompactPercent < 1 || cfg.AutoCompactPercent > 99 {
		return fmt.Errorf("auto_compact_percent must be between 1 and 99")
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
		m.ReasoningEffort = strings.ToLower(strings.TrimSpace(m.ReasoningEffort))

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
		if m.ContextWindow < 0 {
			return fmt.Errorf("model %q context_window must not be negative", m.Name)
		}
		capability := ReasoningCapabilityFor(*m)
		if !capability.Supports(m.ReasoningEffort) {
			return fmt.Errorf("model %q reasoning_effort must be one of %s", m.Name, strings.Join(capability.Values(), ", "))
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
			var err error
			srv.URL, err = utilhttp.NormalizeSecureURL(srv.URL)
			if err != nil {
				return fmt.Errorf("mcp server %q URL: %w", name, err)
			}
			srv.OAuthClientID = strings.TrimSpace(srv.OAuthClientID)
			srv.OAuthClientSecret = strings.TrimSpace(srv.OAuthClientSecret)
			headers, err := utilhttp.NormalizeHeaders(srv.Headers)
			if err != nil {
				return fmt.Errorf("mcp server %q: %w", name, err)
			}
			srv.Headers = headers
			srv.OAuthClientMetadataURL, err = utilhttp.NormalizeSecureURL(srv.OAuthClientMetadataURL)
			if err != nil {
				return fmt.Errorf("mcp server %q OAuth metadata URL: %w", name, err)
			}
			if srv.OAuthCallbackPort < 0 || srv.OAuthCallbackPort > 65535 {
				return fmt.Errorf("invalid MCP OAuth callback port")
			}
			if srv.URL != "" {
				if srv.Command != "" {
					return fmt.Errorf("mcp server %q must specify command or URL, not both", name)
				}
			}
			if srv.Command == "" && srv.URL == "" {
				return fmt.Errorf("mcp server %q command is required", name)
			}
			normalized[name] = srv
		}
		cfg.MCPServers = normalized
	}

	if cfg.Jev != nil {
		cfg.Jev.APIKey = strings.TrimSpace(cfg.Jev.APIKey)
		cfg.Jev.Model = strings.TrimSpace(cfg.Jev.Model)
		cfg.Jev.BaseURL = strings.TrimSpace(cfg.Jev.BaseURL)
		if cfg.Jev.KeepThreshold != nil && (*cfg.Jev.KeepThreshold < 0 || *cfg.Jev.KeepThreshold > 1) {
			return fmt.Errorf("invalid jev keep_threshold: must be between 0 and 1")
		}
		if cfg.Jev.BaseURL != "" {
			if err := utilhttp.ValidateSecureURL(cfg.Jev.BaseURL); err != nil {
				return err
			}
		}
		if cfg.Jev.Model != "" && !strings.HasPrefix(cfg.Jev.Model, "jev-") {
			return fmt.Errorf("invalid jev model %q: expected a jev-* model id", cfg.Jev.Model)
		}
	}

	for i := range cfg.Workspaces {
		cfg.Workspaces[i].Path = strings.TrimSpace(cfg.Workspaces[i].Path)
		cfg.Workspaces[i].Access = strings.TrimSpace(cfg.Workspaces[i].Access)
		if cfg.Workspaces[i].Path == "" {
			return fmt.Errorf("workspace #%d path is required", i+1)
		}
		switch cfg.Workspaces[i].Access {
		case "", "read-only", "read-write":
			if cfg.Workspaces[i].Access == "" {
				cfg.Workspaces[i].Access = "read-only"
			}
		default:
			return fmt.Errorf("workspace %q access must be read-only or read-write", cfg.Workspaces[i].Path)
		}
	}

	return nil
}

var reasoningEfforts = []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"}

// ParseReasoningEffort normalizes a configured or command-line value. Auto
// is the user-facing spelling of the empty provider-default value.
func ParseReasoningEffort(value string) (string, bool) {
	effort := strings.ToLower(strings.TrimSpace(value))
	if effort == "" || effort == "auto" {
		return "", true
	}
	for _, candidate := range reasoningEfforts {
		if effort == candidate {
			return effort, true
		}
	}
	return effort, false
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
