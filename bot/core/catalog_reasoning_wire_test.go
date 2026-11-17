package core

import (
	"encoding/json"
	"nekocode/bot/config"
	"nekocode/bot/modelcatalog"
	"nekocode/bot/provider"
	"nekocode/bot/provider/types"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPublicEffortIsSentThroughExistingProtocolAdapters(t *testing.T) {
	for _, protocol := range []string{"openai", "anthropic"} {
		t.Run(protocol, func(t *testing.T) {
			var body map[string]any
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				w.Header().Set("Content-Type", "application/json")
				if protocol == "anthropic" {
					w.Write([]byte(`{"id":"test","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
				} else {
					w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
				}
			}))
			defer server.Close()
			raw := `{"p":{"api":"` + server.URL + `/v1","models":{"brand-new":{"id":"brand-new","modalities":{"output":["text"]},"reasoning":true,"reasoning_options":[{"type":"effort","values":["low","high"]}],"limit":{"context":96000,"output":16000}}}}}`
			catalog, err := modelcatalog.Decode([]byte(raw))
			if err != nil {
				t.Fatal(err)
			}
			cfg := config.Config{Catalog: catalog, Providers: []config.ProviderConfig{{Name: "p", Protocol: protocol, BaseURL: server.URL + "/v1", Models: []config.ModelDefinition{{Model: "brand-new", ReasoningEffort: "high"}}}}}
			model := cfg.ActiveModelConfig()
			settings, ok := config.ResolveReasoning(model)
			if !ok {
				t.Fatal("public effort could not resolve")
			}
			client := provider.New(provider.Config{Model: model.Model, Protocol: model.Protocol, BaseURL: model.BaseURL, Reasoning: settings, MaxTokens: 32})
			if _, err := client.Chat(t.Context(), []types.Message{{Role: "user", Content: "hello"}}, nil); err != nil {
				t.Fatal(err)
			}
			if protocol == "openai" {
				if body["reasoning_effort"] != "high" {
					t.Fatalf("wire effort=%v", body)
				}
			} else {
				output, ok := body["output_config"].(map[string]any)
				if !ok || output["effort"] != "high" {
					t.Fatalf("wire effort=%v", body)
				}
			}
			if _, exists := body["thinking"]; exists {
				t.Fatal("remote capability invented thinking wire mode")
			}
		})
	}
}
