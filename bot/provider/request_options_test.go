package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"nekocode/bot/provider/types"
)

func TestConcurrentRequestOptionsDoNotChangeClientDefaults(t *testing.T) {
	for _, protocol := range []string{"openai", "anthropic"} {
		t.Run(protocol, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
					w.WriteHeader(400)
					return
				}
				tokens := int(body["max_tokens"].(float64))
				disabled := tokens%2 == 0
				if protocol == "openai" {
					thinking, _ := body["thinking"].(map[string]any)
					if (thinking["type"] == "disabled") != disabled {
						t.Errorf("mixed request options: %v", body)
					}
				} else if (body["output_config"] == nil) != disabled {
					t.Errorf("mixed request options: %v", body)
				}
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"ok"}}],"content":[{"type":"text","text":"ok"}]}`)
			}))
			defer server.Close()
			client := New(Config{BaseURL: server.URL, Model: "test", Protocol: protocol, Reasoning: types.ReasoningSettings{Effort: "high", ThinkingMode: "enabled"}})
			var workers sync.WaitGroup
			for i := 100; i < 120; i++ {
				workers.Go(func() {
					disabled := i%2 == 0
					if _, err := client.(RequestClient).ChatWithOptions(t.Context(), nil, nil, types.RequestOptions{MaxTokens: &i, DisableThinking: &disabled}); err != nil {
						t.Error(err)
					}
				})
			}
			workers.Wait()
			if settings := ReasoningSettingsFor(client); settings.Disabled || settings.Effort != "high" {
				t.Fatalf("defaults changed: %+v", settings)
			}
			if client.(interface{ GetMaxTokens() int }).GetMaxTokens() != 32768 {
				t.Fatal("max token default changed")
			}
		})
	}
}

type legacyRequestClient struct{}

func (legacyRequestClient) Chat(context.Context, []types.Message, []types.ToolDef) (*types.Response, error) {
	return nil, nil
}
func (legacyRequestClient) ChatStream(context.Context, []types.Message, []types.ToolDef) (<-chan types.StreamToken, <-chan error) {
	tokens, errs := make(chan types.StreamToken), make(chan error)
	close(tokens)
	close(errs)
	return tokens, errs
}

func TestLegacyProviderDoesNotSilentlyIgnoreOverrides(t *testing.T) {
	client := legacyRequestClient{}
	disabled := true
	tokens, errs := ChatStream(client, t.Context(), nil, nil, types.RequestOptions{DisableThinking: &disabled})
	if _, ok := <-tokens; ok {
		t.Fatal("unsupported request returned output")
	}
	if err := <-errs; err == nil {
		t.Fatal("unsupported override silently ignored")
	}
	_, errs = ChatStream(client, t.Context(), nil, nil, types.RequestOptions{})
	if err := <-errs; err != nil {
		t.Fatal(err)
	}
}
