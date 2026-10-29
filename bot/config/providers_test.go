package config

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestGroupedModelsResolveAndPersist(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	input := `{
	  "providers": [
	    {"name":"relay","base_url":"https://example.com/v1","api_key":"shared-key","protocol":"openai",
	     "models":["org/model-a",{"name":"coder","model":"gpt-5","reasoning_effort":"high","context_window":96000,"vision":false}, {"model":"model-c","protocol":"anthropic"}]},
	    {"name":"local","base_url":"http://localhost:11434/v1","models":["org/model-a"]}
	  ],
	  "active":"relay/coder",
	  "compaction_model":"local/org/model-a"
	}`
	var cfg Config
	if err := json.Unmarshal([]byte(input), &cfg); err != nil {
		t.Fatal(err)
	}
	if err := Validate(&cfg); err != nil {
		t.Fatal(err)
	}
	model := cfg.ActiveModelConfig()
	if model.Name != "relay/coder" || model.APIKey != "shared-key" || model.BaseURL != "https://example.com/v1" || model.Protocol != "openai" || model.ReasoningEffort != "high" || model.ContextWindow != 96000 || model.Vision == nil || *model.Vision {
		t.Fatalf("lost grouped model settings: %+v", model)
	}
	if got := cfg.ResolveModel(cfg.CompactionModel); got.Provider != "local" || got.Model != "org/model-a" || got.APIKey != "" {
		t.Fatalf("wrong compaction connection: %+v", got)
	}
	if got, ok := cfg.LookupModelConfig("relay/model-c"); !ok || got.Protocol != "anthropic" {
		t.Fatalf("protocol override lost: %+v", got)
	}
	if _, ok := cfg.LookupModelConfig("coder"); ok {
		t.Fatal("unqualified reference accepted")
	}
	if err := Save(cfg); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(Path())
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Count(data, []byte("shared-key")) != 1 || bytes.Contains(data, []byte("flash_model")) {
		t.Fatalf("connection duplicated or obsolete field saved: %s", data)
	}
	var saved map[string]json.RawMessage
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if _, exists := saved["models"]; exists {
		t.Fatal("flattened catalog persisted")
	}
	var groups []struct {
		Models []json.RawMessage `json:"models"`
	}
	if err := json.Unmarshal(saved["providers"], &groups); err != nil {
		t.Fatal(err)
	}
	if string(groups[0].Models[0]) != `"org/model-a"` || !bytes.HasPrefix(groups[0].Models[1], []byte("{")) {
		t.Fatalf("entry shapes changed: %s", data)
	}
	loaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded.AllModels(), cfg.AllModels()) || loaded.CompactionModel != cfg.CompactionModel {
		t.Fatal("resolved settings changed after save/load")
	}
	if !loaded.SwitchModel("relay/org/model-a") {
		t.Fatal("qualified model with slash in ID cannot be selected")
	}
}

func TestGroupedModelValidation(t *testing.T) {
	for _, tc := range []struct{ name, input, want string }{
		{"old models", `{"models":[]}`, "unknown field"},
		{"old flash", `{"flash_model":"fast"}`, "unknown field"},
		{"unknown connection field", `{"providers":[{"name":"p","api_ky":"key","models":["m"]}]}`, "unknown field"},
		{"model credential", `{"providers":[{"name":"p","models":[{"model":"m","api_key":"key"}]}]}`, "unknown field"},
		{"empty providers", `{"providers":[]}`, "at least one provider"},
		{"empty group", `{"providers":[{"name":"p","models":[]}]}`, "at least one model"},
		{"duplicate provider", `{"providers":[{"name":"p","models":["a"]},{"name":"p","models":["b"]}]}`, "duplicate provider"},
		{"invalid provider", `{"providers":[{"name":"a/b","models":["m"]}]}`, "slash"},
		{"duplicate model", `{"providers":[{"name":"p","models":["m",{"name":"m","model":"other"}]}]}`, "duplicate model"},
		{"invalid entry", `{"providers":[{"name":"p","models":[null]}]}`, "string or an object"},
		{"missing ID", `{"providers":[{"name":"p","models":[{}]}]}`, "requires a model ID"},
		{"invalid protocol", `{"providers":[{"name":"p","protocol":"unknown","models":["m"]}]}`, "protocol"},
		{"invalid active", `{"providers":[{"name":"p","models":["m"]}],"active":"missing/m"}`, "active model"},
		{"invalid compaction", `{"providers":[{"name":"p","models":["m"]}],"compaction_model":"m"}`, "compaction model"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var cfg Config
			err := json.Unmarshal([]byte(tc.input), &cfg)
			if err == nil {
				err = Validate(&cfg)
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want %q", err, tc.want)
			}
		})
	}
}

func TestGroupedModelDefaultsAndReasoningStayIndependent(t *testing.T) {
	var cfg Config
	if err := json.Unmarshal([]byte(`{"providers":[{"name":"p","models":["gpt-5",{"model":"other"}]}]}`), &cfg); err != nil {
		t.Fatal(err)
	}
	if err := Validate(&cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Active != "p/gpt-5" || cfg.ResolveModel(cfg.CompactionModel).Name != cfg.Active {
		t.Fatal("default selection or compaction fallback changed")
	}
	clone := cfg.Clone()
	if !clone.SetModelReasoning("p/gpt-5", "high") {
		t.Fatal("model not found")
	}
	if cfg.ActiveModelConfig().ReasoningEffort != "" || clone.ActiveModelConfig().ReasoningEffort != "high" {
		t.Fatal("reasoning update leaked into source config")
	}
	clone.Providers[0].Models[0].Model = "replacement"
	if cfg.Providers[0].Models[0].Model != "gpt-5" {
		t.Fatal("provider model definitions shared")
	}
}

func TestProviderDecodeDoesNotInheritPreviousConnections(t *testing.T) {
	for _, input := range []string{
		`{"providers":[{"name":"private","api_key":"private-secret","models":["gpt-5"]}]}`,
		`{"providers":[{"name":"private","models":["gpt-5"]}]}`,
	} {
		cfg := Default.Clone()
		cfg.Providers[0].APIKey = "previous-secret"
		if err := json.Unmarshal([]byte(input), &cfg); err != nil {
			t.Fatal(err)
		}
		provider := cfg.Providers[0]
		if provider.BaseURL != "" || provider.Protocol != "" || provider.APIKey == "previous-secret" {
			t.Fatalf("new provider inherited an old connection: %+v", provider)
		}
	}
	t.Setenv("HOME", t.TempDir())
	if err := os.MkdirAll(filepath.Dir(Path()), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(Path(), []byte(`{"providers":[{"name":"private","api_key":"private-secret","models":["gpt-5"]}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Active != "private/gpt-5" || loaded.ActiveModelConfig().BaseURL != "" {
		t.Fatal("Load mixed the default connection into a supplied provider")
	}
}

func TestProviderInputRejectsAmbiguousFields(t *testing.T) {
	for _, tc := range []struct{ name, input string }{
		{"mixed case", `{"Providers":[{"name":"private","api_key":"private-secret","models":["gpt-5"]}],"active":"private/gpt-5"}`},
		{"upper case", `{"PROVIDERS":[{"name":"private","api_key":"private-secret","models":["gpt-5"]}],"active":"private/gpt-5"}`},
		{"active case", `{"providers":[{"name":"private","models":["gpt-5"]}],"Active":"private/gpt-5"}`},
		{"duplicate providers", `{"providers":[{"name":"first","base_url":"https://first.example/v1","api_key":"first-secret","models":["m"]}],"providers":[{"name":"second","models":["n"]}]}`},
		{"duplicate active", `{"providers":[{"name":"p","models":["m","n"]}],"active":"p/m","active":"p/n"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Default.Clone()
			before := cfg.Clone()
			if err := json.Unmarshal([]byte(tc.input), &cfg); err == nil {
				t.Errorf("ambiguous configuration was accepted: %+v", cfg.AllModels())
			}
			if !reflect.DeepEqual(cfg, before) {
				t.Error("rejected input mutated the previous configuration")
			}
			t.Setenv("HOME", t.TempDir())
			if err := os.MkdirAll(filepath.Dir(Path()), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(Path(), []byte(tc.input), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(); err == nil {
				t.Fatal("Load accepted ambiguous connection fields")
			}
		})
	}
}
