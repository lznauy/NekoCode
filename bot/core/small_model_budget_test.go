package core

import (
	"context"
	"encoding/json"
	"nekocode/bot/config"
	"nekocode/bot/contextmgr"
	"nekocode/bot/modelcatalog"
	"nekocode/bot/provider"
	"nekocode/bot/provider/types"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSmallModelRequestUsesSameWireAndAdmissionOutputLimit(t *testing.T) {
	wireMax := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			MaxTokens int `json:"max_tokens"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		wireMax = request.MaxTokens
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer server.Close()
	raw := `{"test":{"api":"` + server.URL + `/v1","models":{"small":{"id":"small","modalities":{"output":["text"]},"limit":{"context":8192,"output":1024}}}}}`
	catalog, err := modelcatalog.Decode([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{Catalog: catalog, Providers: []config.ProviderConfig{{Name: "test", BaseURL: server.URL + "/v1", Models: []config.ModelDefinition{{Model: "small"}}}}}
	model := cfg.ActiveModelConfig()
	llm := provider.New(provider.Config{Model: model.Model, BaseURL: model.BaseURL, MaxTokens: model.MaxOutputTokens(32768)})
	manager := contextmgr.New(contextmgr.Config{ContextWindow: model.EffectiveContextWindow()})
	messages, err := manager.PrepareRequest(context.Background(), contextmgr.ModelRequest{OutputTokens: provider.MaxOutputTokensFor(llm), ExtraMessages: []types.Message{{Role: "user", Content: "hello"}}})
	if err != nil {
		t.Fatalf("small model blocked before sending: %v", err)
	}
	if _, err := llm.Chat(t.Context(), messages, nil); err != nil {
		t.Fatal(err)
	}
	if wireMax != 1024 || provider.MaxOutputTokensFor(llm) != wireMax {
		t.Fatalf("wire/admission mismatch: %d / %d", wireMax, provider.MaxOutputTokensFor(llm))
	}
}
