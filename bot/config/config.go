package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"nekocode/bot/extension/tool/runtime/netpolicy"
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
	// Vision controls native image input (multimodal). nil resolves from
	// the built-in model table (false for unknown models); an explicit
	// value always overrides it. When enabled, pasted images are sent to
	// the model as image content parts instead of only being available
	// through the image_understand tool.
	Vision *bool `json:"vision,omitempty"`
}

type ImageGenConfig struct {
	Name      string `json:"name"`
	Provider  string `json:"provider"`
	APIKey    string `json:"api_key"`
	SecretKey string `json:"secret_key"`
	BaseURL   string `json:"base_url,omitempty"`
	Model     string `json:"model,omitempty"`
}

// ImageUnderstandConfig describes a multimodal model used by the
// image_understand tool. Provider is a user-facing label; Protocol controls
// the wire format and defaults to openai when omitted.
type ImageUnderstandConfig struct {
	Name     string `json:"name"`
	Provider string `json:"provider"`
	APIKey   string `json:"api_key"`
	Model    string `json:"model"`
	BaseURL  string `json:"base_url,omitempty"`
	Protocol string `json:"protocol,omitempty"`
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
	SandboxMode     string   `json:"sandbox_mode,omitempty"`
	Network         bool     `json:"network,omitempty"`
	OutboundDomains []string `json:"outbound_domains"`
	WritableRoots   []string `json:"writable_roots,omitempty"`
}

type WorkspaceConfig struct {
	Path   string `json:"path"`
	Access string `json:"access,omitempty"`
}

type Config struct {
	Active                 string                     `json:"active"`                     // name of the active model
	CompactionModel        string                     `json:"compaction_model,omitempty"` // optional context-compaction model reference; empty uses the active model
	AutoCompactPercent     int                        `json:"auto_compact_percent,omitempty"`
	MaxConcurrentSubagents int                        `json:"max_concurrent_subagents,omitempty"`
	Providers              []ProviderConfig           `json:"providers"`
	ImageGenModels         []ImageGenConfig           `json:"image_gen_models,omitempty"`        // text-to-image models
	ImageUnderstandModels  []ImageUnderstandConfig    `json:"image_understand_models,omitempty"` // image-to-text models
	MCPServers             map[string]MCPServerConfig `json:"mcp_servers,omitempty"`
	Permissions            *PermissionsConfig         `json:"permissions,omitempty"`
	Workspaces             []WorkspaceConfig          `json:"workspaces,omitempty"`
	Jev                    *JevConfig                 `json:"jev,omitempty"` // optional "System One" decision engine (e.g. context-compaction relevance pruning)
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

const DefaultMaxConcurrentSubagents = 8

var Default = Config{
	Active:                 "deepseek/default",
	AutoCompactPercent:     DefaultAutoCompactPercent,
	MaxConcurrentSubagents: DefaultMaxConcurrentSubagents,
	Providers: []ProviderConfig{{
		Name: "deepseek", BaseURL: "https://api.deepseek.com/v1", Protocol: "openai",
		Models: []ModelDefinition{{Name: "default", Model: "deepseek-v4-flash"}},
	}},
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
	if os.IsNotExist(err) {
		return &cfg, nil
	}
	if err != nil {
		return nil, err
	}
	_ = os.Chmod(configPath, 0o600)

	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("config %s: %w", configPath, err)
	}
	if err := Validate(&cfg); err != nil {
		return nil, fmt.Errorf("config %s: %w", configPath, err)
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

func (c *Config) EffectiveMaxConcurrentSubagents() int {
	if c.MaxConcurrentSubagents > 0 {
		return c.MaxConcurrentSubagents
	}
	return DefaultMaxConcurrentSubagents
}

// Clone returns an independently mutable configuration value.
func (c Config) Clone() Config {
	out := c
	out.Providers = slices.Clone(c.Providers)
	for i := range out.Providers {
		out.Providers[i].Models = slices.Clone(c.Providers[i].Models)
		for j := range out.Providers[i].Models {
			if value := c.Providers[i].Models[j].Vision; value != nil {
				vision := *value
				out.Providers[i].Models[j].Vision = &vision
			}
		}
	}
	out.ImageGenModels = append([]ImageGenConfig(nil), c.ImageGenModels...)
	out.ImageUnderstandModels = append([]ImageUnderstandConfig(nil), c.ImageUnderstandModels...)
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
				sandbox.OutboundDomains = slices.Clone(sandbox.OutboundDomains)
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

// Save validates an independent copy and atomically replaces the configuration.
// Errors are returned before replacement; a successful rename commits the save.
func Save(cfg Config) error {
	cfg = cfg.Clone()
	if err := Validate(&cfg); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	path := Path()
	// Resolve the final symlink chain, including links to a not-yet-created
	// file, so replacement updates the target without destroying the link.
	seen := make(map[string]bool)
	for {
		// Split preserves .. until directory links have been resolved.
		parent, name := filepath.Split(path)
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return err
		}
		parent, err := filepath.EvalSymlinks(parent)
		if err != nil {
			return err
		}
		path = filepath.Join(parent, name)
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			break
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink == 0 {
			break
		}
		if seen[path] {
			return fmt.Errorf("configuration symlink cycle at %q", path)
		}
		seen[path] = true
		target, err := os.Readlink(path)
		if err != nil {
			return err
		}
		if !filepath.IsAbs(target) {
			target = parent + string(filepath.Separator) + target
		}
		path = target
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".config-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func Validate(cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("config is nil")
	}
	if cfg.Permissions != nil {
		for rule, profile := range cfg.Permissions.Sandbox {
			domains, err := netpolicy.Normalize(profile.OutboundDomains)
			if err != nil {
				return fmt.Errorf("permissions.sandbox %q: %w", rule, err)
			}
			if domains != nil && strings.TrimSpace(profile.SandboxMode) == "host" {
				return fmt.Errorf("permissions.sandbox %q: %s", rule, `outbound_domains cannot be used with host execution: host mode cannot enforce domain restrictions; use sandbox_mode="workspace-write" or "read-only". If unrestricted host execution is intended, remove outbound_domains from both the tool arguments and matching permissions.sandbox rules (an empty array still requires isolation)`)
			}
			profile.OutboundDomains = domains
			cfg.Permissions.Sandbox[rule] = profile
		}
	}
	if err := cfg.validateProviders(); err != nil {
		return err
	}
	if cfg.AutoCompactPercent == 0 {
		cfg.AutoCompactPercent = DefaultAutoCompactPercent
	}
	if cfg.AutoCompactPercent < 1 || cfg.AutoCompactPercent > 99 {
		return fmt.Errorf("auto_compact_percent must be between 1 and 99")
	}
	if cfg.MaxConcurrentSubagents < 0 {
		return fmt.Errorf("max_concurrent_subagents must be a positive integer (0 uses the default)")
	}
	if cfg.MaxConcurrentSubagents == 0 {
		cfg.MaxConcurrentSubagents = DefaultMaxConcurrentSubagents
	}

	models := cfg.AllModels()
	seen := make(map[string]bool, len(models))
	for _, model := range models {
		seen[model.Name] = true
	}
	cfg.Active = strings.TrimSpace(cfg.Active)
	if cfg.Active == "" {
		cfg.Active = models[0].Name
	}
	if !seen[cfg.Active] {
		return fmt.Errorf("active model %q does not exist; use provider/model", cfg.Active)
	}
	cfg.CompactionModel = strings.TrimSpace(cfg.CompactionModel)
	if cfg.CompactionModel != "" && !seen[cfg.CompactionModel] {
		return fmt.Errorf("compaction model %q does not exist; use provider/model", cfg.CompactionModel)
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

	imageUnderstandSeen := make(map[string]bool, len(cfg.ImageUnderstandModels))
	for i := range cfg.ImageUnderstandModels {
		m := &cfg.ImageUnderstandModels[i]
		m.Name = strings.TrimSpace(m.Name)
		m.Provider = strings.TrimSpace(m.Provider)
		m.APIKey = strings.TrimSpace(m.APIKey)
		m.Model = strings.TrimSpace(m.Model)
		var err error
		m.BaseURL, err = utilhttp.NormalizeSecureURL(m.BaseURL)
		if err != nil {
			return fmt.Errorf("image understand model %q base URL: %w", m.Name, err)
		}
		m.Protocol = strings.ToLower(strings.TrimSpace(m.Protocol))
		if m.Name == "" {
			return fmt.Errorf("image understand model #%d name is required", i+1)
		}
		nameKey := strings.ToLower(m.Name)
		if imageUnderstandSeen[nameKey] {
			return fmt.Errorf("duplicate image understand model name %q", m.Name)
		}
		imageUnderstandSeen[nameKey] = true
		if m.Provider == "" {
			return fmt.Errorf("image understand model %q provider is required", m.Name)
		}
		if m.Model == "" {
			return fmt.Errorf("image understand model %q model id is required", m.Name)
		}
		if m.Protocol != "" && m.Protocol != "openai" && m.Protocol != "anthropic" {
			return fmt.Errorf("image understand model %q protocol must be openai or anthropic", m.Name)
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
	for _, m := range c.AllModels() {
		if m.Name == c.Active {
			return m
		}
	}
	// Fallback to first model if active not found
	if len(c.AllModels()) > 0 {
		return c.AllModels()[0]
	}
	return ModelConfig{}
}

// LookupModelConfig returns the ModelConfig for a named model.
func (c *Config) LookupModelConfig(name string) (ModelConfig, bool) {
	for _, m := range c.AllModels() {
		if m.Name == name {
			return m, true
		}
	}
	return ModelConfig{}, false
}

// AllModelNames returns all available model names.
func (c *Config) AllModelNames() []string {
	names := make([]string, 0, len(c.AllModels()))
	for _, m := range c.AllModels() {
		names = append(names, m.Name)
	}
	return names
}

// SwitchModel switches to the named model. Returns false if not found.
func (c *Config) SwitchModel(name string) bool {
	for _, m := range c.AllModels() {
		if m.Name == name {
			c.Active = name
			return true
		}
	}
	return false
}
