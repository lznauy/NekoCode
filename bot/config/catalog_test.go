package config

import (
	"encoding/json"
	"nekocode/bot/modelcatalog"
	"strings"
	"testing"
)

func TestCatalogLimitsRespectOverridesAndDoNotChangeProtocols(t *testing.T) {
	catalog, err := modelcatalog.Decode([]byte(`{"p":{"api":"https://service.example/v1","models":{"brand-new-model":{"id":"brand-new-model","modalities":{"input":["image"],"output":["text"]},"reasoning":true,"limit":{"context":96000,"output":16000}}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	cfg := Config{Catalog: catalog, Active: "my-relay/new", Providers: []ProviderConfig{{Name: "my-relay", BaseURL: "https://service.example/v1", Models: []ModelDefinition{{Name: "new", Model: "brand-new-model"}}}}}
	model := cfg.ActiveModelConfig()
	if n, source := model.ResolveContextWindow(); n != 96000 || source != "catalog" {
		t.Fatalf("catalog ignored: %d %s", n, source)
	}
	if model.EffectiveVision() {
		t.Fatal("remote catalog enabled native image sending")
	}
	cfg.Providers[0].Models[0].ContextWindow = 64000
	if n, source := cfg.ActiveModelConfig().ResolveContextWindow(); n != 64000 || source != "override" {
		t.Fatalf("explicit override lost: %d %s", n, source)
	}
	cfg.Providers[0].BaseURL = "https://unrelated-proxy.example/v1"
	cfg.Providers[0].Models[0].ContextWindow = 0
	if cfg.EffectiveContextWindow() != DefaultContextWindow {
		t.Fatal("model name alone matched unrelated endpoint")
	}
	if model.EffectiveContextWindow() != 96000 {
		t.Fatal("resolved model changed after configuration edit")
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "endpoints") || strings.Contains(string(data), "fetched_at") || strings.Contains(string(data), "context_window") {
		t.Fatalf("resolved metadata persisted as a user override: %s", data)
	}
}

func TestCatalogInputCeilingConstrainsContextBudget(t *testing.T) {
	catalog, err := modelcatalog.Decode([]byte(`{"p":{"api":"https://service.example/v1","models":{"limited":{"id":"limited","modalities":{"output":["text"]},"limit":{"context":100000,"input":80000,"output":16000}}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	cfg := Config{Catalog: catalog, Providers: []ProviderConfig{{Name: "p", BaseURL: "https://service.example/v1", Models: []ModelDefinition{{Model: "limited"}}}}}
	if cfg.EffectiveContextWindow() != 80000 {
		t.Fatal("public input ceiling was ignored")
	}
}

func TestSmallModelOutputBudgetFitsWindowAndCatalogCeiling(t *testing.T) {
	catalog, err := modelcatalog.Decode([]byte(`{"p":{"api":"https://small.example/v1","models":{"small":{"id":"small","modalities":{"output":["text"]},"limit":{"context":8192,"output":1024}}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	cfg := Config{Catalog: catalog, Providers: []ProviderConfig{{Name: "p", BaseURL: "https://small.example/v1", Models: []ModelDefinition{{Model: "small"}}}}}
	m := cfg.ActiveModelConfig()
	if got := m.MaxOutputTokens(32768); got != 1024 {
		t.Fatalf("output cap=%d want 1024", got)
	}
	if got := m.MaxOutputTokens(500); got != 500 {
		t.Fatalf("explicit smaller cap increased: %d", got)
	}
	m.ContextWindow = 2048
	if got := m.MaxOutputTokens(32768); got != 512 {
		t.Fatalf("context override ignored: %d", got)
	}
}
