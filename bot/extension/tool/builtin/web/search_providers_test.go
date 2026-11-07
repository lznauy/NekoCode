package web

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
)

func TestStructuredSearchFiltersDeduplicatesAndAuthenticates(t *testing.T) {
	t.Setenv("EXA_API_KEY", "test-exa")
	t.Setenv("TAVILY_API_KEY", "test-tavily")
	for _, provider := range []string{"exa", "tavily"} {
		tool := &WebSearchTool{client: &http.Client{Transport: webRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			var args map[string]any
			if err := json.NewDecoder(req.Body).Decode(&args); err != nil {
				t.Fatal(err)
			}
			if provider == "exa" {
				if req.Header.Get("X-Api-Key") != "test-exa" || args["includeDomains"] == nil || args["contents"] == nil {
					t.Fatal("Exa request contract missing")
				}
			} else if req.Header.Get("Authorization") != "Bearer test-tavily" || args["include_domains"] == nil || args["search_depth"] != "basic" || args["include_answer"] != false {
				t.Fatal("Tavily request contract missing")
			}
			return stubResponse(200, `{"results":[{"title":"Official","url":"https://docs.example.com/guide?utm_source=a#first","highlights":["verified excerpt"],"content":"verified excerpt"},{"url":"https://docs.example.com/guide#second"},{"url":"https://docs.example.com.evil.test/"},{"url":"javascript:alert(1)"},{"url":"https://user:secret@example.com/"}]}`), nil
		})}}
		got, err := tool.Execute(context.Background(), map[string]any{"query": "guide", "provider": provider, "domains": []any{"EXAMPLE.COM"}})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Count(got.Text, "https://docs.example.com/guide") != 1 || !strings.Contains(got.Text, "verified excerpt") || strings.Contains(got.Text, "evil.test") || strings.Contains(got.Text, "javascript:") || strings.Contains(got.Text, "secret") {
			t.Fatalf("bad sources: %s", got.Text)
		}
	}
}

func TestSearchFailoverCooldownAndExplicitProvider(t *testing.T) {
	t.Setenv("EXA_API_KEY", "test-exa")
	t.Setenv("TAVILY_API_KEY", "test-tavily")
	exaCalls, tavilyCalls := 0, 0
	tool := &WebSearchTool{client: &http.Client{Transport: webRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == "api.exa.ai" {
			exaCalls++
			r := stubResponse(429, "rate limited")
			r.Header.Set("Retry-After", "120")
			return r, nil
		}
		tavilyCalls++
		return stubResponse(200, `{"results":[{"title":"Backup","url":"https://example.com/source","content":"source excerpt"}]}`), nil
	})}}
	for range 2 {
		got, err := tool.Execute(context.Background(), map[string]any{"query": "source"})
		if err != nil || !strings.Contains(got.Text, "switched") || !strings.Contains(got.Text, "tavily") {
			t.Fatalf("no fallback disclosure: %q %v", got.Text, err)
		}
	}
	if exaCalls != 1 || tavilyCalls != 2 {
		t.Fatalf("cooldown ignored: exa=%d tavily=%d", exaCalls, tavilyCalls)
	}
	if _, err := tool.Execute(context.Background(), map[string]any{"query": "source", "provider": "exa"}); err == nil || tavilyCalls != 2 {
		t.Fatal("explicit provider must not switch")
	}
}

func TestSearchNeverDropsFiltersOrFailsOverAfterCancellation(t *testing.T) {
	t.Setenv("EXA_API_KEY", "")
	t.Setenv("TAVILY_API_KEY", "")
	tool := &WebSearchTool{client: &http.Client{Transport: webRoundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("unconfigured filtered search reached network")
		return nil, nil
	})}}
	if _, err := tool.Execute(context.Background(), map[string]any{"query": "source", "domains": []string{"example.com"}}); err == nil || !strings.Contains(err.Error(), "never silently removed") {
		t.Fatalf("filters dropped: %v", err)
	}
	t.Setenv("EXA_API_KEY", "test-exa")
	t.Setenv("TAVILY_API_KEY", "test-tavily")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	tool.client.Transport = webRoundTripFunc(func(*http.Request) (*http.Response, error) { calls++; cancel(); return nil, ctx.Err() })
	if _, err := tool.Execute(ctx, map[string]any{"query": "source"}); err == nil || calls != 1 {
		t.Fatalf("cancellation retried: calls=%d err=%v", calls, err)
	}
}

func TestSearchPreservesLongUnfilteredProviderResponse(t *testing.T) {
	t.Setenv("EXA_API_KEY", "")
	t.Setenv("TAVILY_API_KEY", "")
	body, _ := json.Marshal(map[string]any{"result": map[string]any{"content": []map[string]string{{"text": strings.Repeat("head ", 4000) + "exact omitted tail"}}}})
	tool := &WebSearchTool{client: &http.Client{Transport: webRoundTripFunc(func(*http.Request) (*http.Response, error) {
		r := stubResponse(200, string(body))
		r.Header.Set("Content-Type", "application/json")
		return r, nil
	})}}
	got, err := tool.Execute(context.Background(), map[string]any{"query": "source"})
	if err != nil || got.Artifact.Path == "" {
		t.Fatalf("source not preserved: %+v %v", got, err)
	}
	full, err := os.ReadFile(got.Artifact.Path)
	if err != nil || !strings.HasSuffix(string(full), "exact omitted tail") {
		t.Fatal("parser discarded tail before storage")
	}
}
