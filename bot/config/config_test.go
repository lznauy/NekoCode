package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMaxConcurrentSubagentsConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg, err := Load()
	if err != nil || cfg.EffectiveMaxConcurrentSubagents() != 8 {
		t.Fatalf("default=%+v err=%v", cfg, err)
	}
	for _, limit := range []int{1, 12, 100} {
		cfg.MaxConcurrentSubagents = limit
		if err := Save(*cfg); err != nil {
			t.Fatal(err)
		}
		loaded, err := Load()
		if err != nil || loaded.MaxConcurrentSubagents != limit || loaded.EffectiveMaxConcurrentSubagents() != limit {
			t.Fatalf("limit=%d config=%+v err=%v", limit, loaded, err)
		}
	}
	for _, text := range []string{`{}`, `{"max_concurrent_subagents":0}`} {
		if err := os.WriteFile(Path(), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
		loaded, err := Load()
		if err != nil || loaded.EffectiveMaxConcurrentSubagents() != 8 {
			t.Fatal("missing/zero must use default")
		}
	}
	cfg.MaxConcurrentSubagents = -1
	if err := Validate(cfg); err == nil {
		t.Fatal("negative concurrency accepted")
	}
}

func TestDefaultConfig(t *testing.T) {
	if Default.Active != "deepseek/default" {
		t.Errorf("expected Active 'default', got '%s'", Default.Active)
	}
	if len(Default.AllModels()) != 1 {
		t.Fatalf("expected 1 model, got %d", len(Default.AllModels()))
	}
	m := Default.AllModels()[0]
	if m.Provider != "deepseek" {
		t.Errorf("expected Provider 'deepseek', got '%s'", m.Provider)
	}
	if m.Model != "deepseek-v4-flash" {
		t.Errorf("expected Model 'deepseek-v4-flash', got '%s'", m.Model)
	}
	if m.BaseURL != "https://api.deepseek.com/v1" {
		t.Errorf("expected BaseURL 'https://api.deepseek.com/v1', got '%s'", m.BaseURL)
	}
	if DefaultContextWindow != 128000 {
		t.Errorf("expected DefaultContextWindow 128000, got %d", DefaultContextWindow)
	}
	if Default.AutoCompactPercent != 80 {
		t.Errorf("expected AutoCompactPercent 80, got %d", Default.AutoCompactPercent)
	}
	if Default.CompactionModel != "" {
		t.Errorf("expected CompactionModel empty, got %s", Default.CompactionModel)
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
		Active:          "anthropic/claude",
		CompactionModel: "deepseek/default",
		Providers:       []ProviderConfig{{Name: "deepseek", APIKey: "sk-ds", BaseURL: "https://api.deepseek.com/v1", Models: []ModelDefinition{{Name: "default", Model: "deepseek-chat"}}}, {Name: "anthropic", APIKey: "sk-ant", BaseURL: "https://api.anthropic.com", Protocol: "anthropic", Models: []ModelDefinition{{Name: "claude", Model: "claude-3-opus"}}}},
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
	if info, err := os.Stat(configPath); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("config mode after load = %v, %v; want 600", info, err)
	}

	if cfg.Active != customCfg.Active {
		t.Errorf("expected Active '%s', got '%s'", customCfg.Active, cfg.Active)
	}
	if cfg.CompactionModel != customCfg.CompactionModel {
		t.Errorf("expected CompactionModel %s, got %s", customCfg.CompactionModel, cfg.CompactionModel)
	}
	if len(cfg.AllModels()) != 2 {
		t.Fatalf("expected 2 models, got %d", len(cfg.AllModels()))
	}

	am := cfg.ActiveModelConfig()
	if am.Name != "anthropic/claude" || am.Provider != "anthropic" || am.Model != "claude-3-opus" {
		t.Errorf("unexpected active config: %+v", am)
	}
}

func TestLoad_InvalidJSON(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := os.MkdirAll(filepath.Dir(Path()), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(Path(), []byte("not valid json {{{"), 0600); err != nil {
		t.Fatal(err)
	}
	if cfg, err := Load(); err == nil || cfg != nil {
		t.Fatalf("invalid config silently accepted: cfg=%v err=%v", cfg, err)
	}
}

func TestLoad_PartialConfig(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	configDir := filepath.Join(tmpDir, ".nekocode")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}

	partialJSON := `{"active": "openai/gpt4", "providers": [{"name": "openai", "api_key": "sk-xxx", "base_url": "", "protocol": "", "models": [{"name": "gpt4", "model": "gpt-4-turbo"}]}]}`
	configPath := filepath.Join(configDir, "config.json")
	if err := os.WriteFile(configPath, []byte(partialJSON), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	if cfg.Active != "openai/gpt4" {
		t.Errorf("expected Active 'gpt4', got '%s'", cfg.Active)
	}
	// The effective window resolves from the built-in table
	// (gpt-4-turbo → 128K).
	if cfg.EffectiveContextWindow() != 128000 {
		t.Errorf("expected effective ContextWindow 128000, got %d", cfg.EffectiveContextWindow())
	}
}

func TestConfig_JSONRoundTrip(t *testing.T) {
	original := Config{
		Active:             "openai/default",
		CompactionModel:    "flash",
		AutoCompactPercent: 75,
		Providers:          []ProviderConfig{{Name: "openai", APIKey: "sk-abc", BaseURL: "https://custom.api.com/v1", Models: []ModelDefinition{{Name: "default", Model: "gpt-4-turbo"}}}},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var restored Config
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if restored.Active != original.Active || restored.CompactionModel != original.CompactionModel || restored.AutoCompactPercent != original.AutoCompactPercent {
		t.Errorf("round-trip failed: got %+v, want %+v", restored, original)
	}
	if len(restored.AllModels()) != 1 || restored.Providers[0].Models[0].Name != "default" {
		t.Errorf("models round-trip failed")
	}
}

func TestConfigCloneDoesNotShareMutableState(t *testing.T) {
	original := Config{
		Providers:             []ProviderConfig{{Name: "test", Models: []ModelDefinition{{Name: "model"}}}},
		ImageUnderstandModels: []ImageUnderstandConfig{{Name: "vision"}},
		MCPServers: map[string]MCPServerConfig{
			"server": {Args: []string{"first"}, Env: map[string]string{"KEY": "value"}},
		},
		Permissions: &PermissionsConfig{
			Allow: []string{"read"},
			Sandbox: map[string]SandboxConfig{
				"shell": {WritableRoots: []string{"/work"}},
			},
		},
	}

	clone := original.Clone()
	clone.Providers[0].Models[0].Name = "changed"
	clone.ImageUnderstandModels[0].Name = "changed"
	server := clone.MCPServers["server"]
	server.Args[0] = "changed"
	server.Env["KEY"] = "changed"
	clone.MCPServers["server"] = server
	clone.Permissions.Allow[0] = "changed"
	sandbox := clone.Permissions.Sandbox["shell"]
	sandbox.WritableRoots[0] = "/changed"
	clone.Permissions.Sandbox["shell"] = sandbox

	if original.Providers[0].Models[0].Name != "model" ||
		original.ImageUnderstandModels[0].Name != "vision" ||
		original.MCPServers["server"].Args[0] != "first" ||
		original.MCPServers["server"].Env["KEY"] != "value" ||
		original.Permissions.Allow[0] != "read" ||
		original.Permissions.Sandbox["shell"].WritableRoots[0] != "/work" {
		t.Fatalf("clone mutated original: %+v", original)
	}
}

func TestValidateImageUnderstandModels(t *testing.T) {
	cfg := Config{
		Providers: []ProviderConfig{{Name: "openai", Models: []ModelDefinition{{Name: "default", Model: "gpt-5"}}}},
		ImageUnderstandModels: []ImageUnderstandConfig{{
			Name: " vision ", Provider: " openai ", Model: " gpt-5 ", Protocol: " OPENAI ",
		}},
	}
	if err := Validate(&cfg); err != nil {
		t.Fatal(err)
	}
	if got := cfg.ImageUnderstandModels[0]; got.Name != "vision" || got.Protocol != "openai" {
		t.Fatalf("image understand config was not normalized: %+v", got)
	}
	cfg.ImageUnderstandModels[0].Protocol = "unknown"
	if err := Validate(&cfg); err == nil {
		t.Fatal("unsupported image understand protocol should fail validation")
	}
	cfg.ImageUnderstandModels[0].Protocol = "openai"
	cfg.ImageUnderstandModels[0].BaseURL = "http://example.com/v1"
	if err := Validate(&cfg); err == nil {
		t.Fatal("insecure image understand base URL should fail validation")
	}

	duplicate := Config{
		Providers: []ProviderConfig{{Name: "openai", Models: []ModelDefinition{{Name: "default", Model: "gpt-5"}}}},
		ImageUnderstandModels: []ImageUnderstandConfig{
			{Name: "fast", Provider: "openai", Model: "vision-fast"},
			{Name: "FAST", Provider: "openai", Model: "vision-other"},
		},
	}
	if err := Validate(&duplicate); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("case-insensitive duplicate image model name should fail validation: %v", err)
	}
}

func TestSave_CreatesConfigFile(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	cfg := Config{
		Active:    "openai/default",
		Providers: []ProviderConfig{{Name: "openai", Protocol: "openai", Models: []ModelDefinition{{Name: "default", Model: "gpt-4o-mini"}}}},
	}

	if err := Save(cfg); err != nil {
		t.Fatalf("Save() returned error: %v", err)
	}
	if !Exists() {
		t.Fatal("expected config file to exist")
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if loaded.Active != "openai/default" {
		t.Fatalf("unexpected saved config: %+v", loaded)
	}
}

func TestValidate_DuplicateModelName(t *testing.T) {
	cfg := Config{
		Active:    "openai/default",
		Providers: []ProviderConfig{{Name: "openai", Models: []ModelDefinition{{Name: "default", Model: "gpt-4o-mini"}, {Name: "default", Model: "gpt-5"}}}},
	}

	if err := Validate(&cfg); err == nil {
		t.Fatal("expected duplicate model name error")
	}
}

func TestValidateReasoningEffort(t *testing.T) {
	cfg := Config{Providers: []ProviderConfig{{Name: "deepseek", Models: []ModelDefinition{{Name: "default", Model: "deepseek-v4-flash", ReasoningEffort: " HIGH "}}}}}
	if err := Validate(&cfg); err != nil {
		t.Fatal(err)
	}
	if got := cfg.Providers[0].Models[0].ReasoningEffort; got != "high" {
		t.Fatalf("normalized reasoning_effort = %q, want high", got)
	}
	cfg.Providers[0].Models[0].ReasoningEffort = "extreme"
	if err := Validate(&cfg); err == nil {
		t.Fatal("unsupported reasoning_effort should fail validation")
	}
}

func TestValidateAutoCompactPercent(t *testing.T) {
	cfg := Config{Providers: []ProviderConfig{{Name: "openai", Models: []ModelDefinition{{Name: "default", Model: "gpt-4o"}}}}}
	if err := Validate(&cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.AutoCompactPercent != DefaultAutoCompactPercent {
		t.Fatalf("default auto_compact_percent = %d, want %d", cfg.AutoCompactPercent, DefaultAutoCompactPercent)
	}

	cfg.AutoCompactPercent = 100
	if err := Validate(&cfg); err == nil {
		t.Fatal("auto_compact_percent above 99 should fail validation")
	}
}

func TestConfig_ModelsList(t *testing.T) {
	cfg := Config{
		Active:    "openai/default",
		Providers: []ProviderConfig{{Name: "deepseek", APIKey: "sk-ds", BaseURL: "https://api.deepseek.com/v1", Models: []ModelDefinition{{Name: "default", Model: "deepseek-chat"}}}, {Name: "anthropic", APIKey: "sk-ant", BaseURL: "https://api.anthropic.com", Protocol: "anthropic", Models: []ModelDefinition{{Name: "claude", Model: "claude-sonnet-4-6"}}}, {Name: "openai", APIKey: "sk-openai", Models: []ModelDefinition{{Name: "gpt4", Model: "gpt-4-turbo"}}}},
	}

	names := cfg.AllModelNames()
	if len(names) != 3 {
		t.Fatalf("expected 3 model names, got %d: %v", len(names), names)
	}
	if names[0] != "deepseek/default" || names[1] != "anthropic/claude" || names[2] != "openai/gpt4" {
		t.Errorf("unexpected names: %v", names)
	}

	am := cfg.ActiveModelConfig()
	if am.Name != "deepseek/default" || am.Provider != "deepseek" || am.Model != "deepseek-chat" {
		t.Errorf("unexpected active config: %+v", am)
	}

	if !cfg.SwitchModel("anthropic/claude") {
		t.Fatal("SwitchModel(claude) failed")
	}
	if cfg.Active != "anthropic/claude" {
		t.Errorf("expected Active 'claude', got '%s'", cfg.Active)
	}

	am = cfg.ActiveModelConfig()
	if am.Name != "anthropic/claude" || am.Provider != "anthropic" || am.APIKey != "sk-ant" || am.Model != "claude-sonnet-4-6" {
		t.Errorf("SwitchModel(claude) active config wrong: %+v", am)
	}

	if !cfg.SwitchModel("openai/gpt4") {
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
		Active:    "anthropic/claude",
		Providers: []ProviderConfig{{Name: "deepseek", APIKey: "sk-ds", BaseURL: "https://api.deepseek.com/v1", Models: []ModelDefinition{{Name: "default", Model: "deepseek-chat"}}}, {Name: "anthropic", APIKey: "sk-ant", Protocol: "anthropic", Models: []ModelDefinition{{Name: "claude", Model: "claude-sonnet-4-6", ReasoningEffort: "medium"}}}},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var restored Config
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if got := restored.AllModels()[1].ReasoningEffort; got != "medium" {
		t.Fatalf("reasoning_effort round trip = %q", got)
	}

	if len(restored.AllModels()) != 2 {
		t.Fatalf("expected 2 models, got %d", len(restored.AllModels()))
	}
	if restored.Active != "anthropic/claude" {
		t.Errorf("expected active 'claude', got %s", restored.Active)
	}
}

func TestOutboundDomainsValidateAndClone(t *testing.T) {
	cfg := Config{Active: "openai/test", Providers: []ProviderConfig{{Name: "openai", APIKey: "fixture", Models: []ModelDefinition{{Name: "test", Model: "test"}}}}, Permissions: &PermissionsConfig{Sandbox: map[string]SandboxConfig{"shell": {OutboundDomains: []string{"EXAMPLE.COM."}}, "shell(false)": {OutboundDomains: []string{}}}}}
	if err := Validate(&cfg); err != nil {
		t.Fatal(err)
	}
	cloned := cfg.Clone()
	entry := cloned.Permissions.Sandbox["shell"]
	entry.OutboundDomains[0] = "changed.example"
	if cfg.Permissions.Sandbox["shell"].OutboundDomains[0] != "example.com" || cloned.Permissions.Sandbox["shell(false)"].OutboundDomains == nil {
		t.Fatal("domain policy clone lost isolation")
	}
	cfg.Permissions.Sandbox["shell"] = SandboxConfig{OutboundDomains: []string{"*.example.com"}}
	if err := Validate(&cfg); err == nil {
		t.Fatal("wildcard allowed")
	}
	cfg.Permissions.Sandbox["shell"] = SandboxConfig{SandboxMode: "host", OutboundDomains: []string{}}
	if err := Validate(&cfg); err == nil {
		t.Fatal("host restriction allowed")
	}
}
