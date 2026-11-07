package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"nekocode/bot/extension/tool/runtime/netpolicy"
)

type searchOptions struct {
	provider string
	domains  []string
}
type searchHit struct {
	Title         string   `json:"title"`
	URL           string   `json:"url"`
	Text          string   `json:"text"`
	Content       string   `json:"content"`
	Highlights    []string `json:"highlights"`
	PublishedDate string   `json:"publishedDate"`
}
type searchHTTPError struct {
	provider string
	status   int
	cooldown time.Duration
}

func (e *searchHTTPError) Error() string { return fmt.Sprintf("%s: HTTP %d", e.provider, e.status) }

func parseSearchOptions(args map[string]any) (searchOptions, error) {
	o := searchOptions{provider: "auto"}
	if raw, ok := args["provider"]; ok {
		value, valid := raw.(string)
		if !valid || (value != "auto" && value != "exa" && value != "tavily") {
			return o, fmt.Errorf("provider must be auto, exa or tavily")
		}
		o.provider = value
	}
	if raw, ok := args["domains"]; ok {
		domains, err := netpolicy.Parse(raw)
		if err != nil || len(domains) == 0 || len(domains) > 20 {
			return o, fmt.Errorf("domains must contain 1–20 plain DNS names")
		}
		o.domains = domains
	}
	return o, nil
}

func (t *WebSearchTool) searchProviders(ctx context.Context, query string, n int, o searchOptions) (string, string, bool, error) {
	providers := []string{o.provider}
	if o.provider == "auto" {
		providers = nil
		if len(o.domains) == 0 || os.Getenv("EXA_API_KEY") != "" {
			providers = append(providers, "exa")
		}
		if os.Getenv("TAVILY_API_KEY") != "" {
			providers = append(providers, "tavily")
		}
	}
	var failures []string
	for _, provider := range providers {
		if err := ctx.Err(); err != nil {
			return "", "", false, err
		}
		if t.searchCoolingDown(provider) {
			failures = append(failures, provider+": temporarily rate limited")
			continue
		}
		hopCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		var content, method string
		var err error
		switch provider {
		case "exa":
			if key := os.Getenv("EXA_API_KEY"); key != "" {
				content, err = t.searchJSON(hopCtx, provider, query, n, o.domains, key)
				method = "exa"
			} else if len(o.domains) > 0 {
				err = fmt.Errorf("Exa domain filtering requires EXA_API_KEY")
			} else {
				content, err = t.searchExa(hopCtx, query, n)
				method = "exa-mcp"
			}
		case "tavily":
			if key := os.Getenv("TAVILY_API_KEY"); key != "" {
				content, err = t.searchJSON(hopCtx, provider, query, n, o.domains, key)
				method = "tavily"
			} else {
				err = fmt.Errorf("TAVILY_API_KEY is not configured")
			}
		}
		cancel()
		if err == nil {
			return content, method, len(failures) > 0, nil
		}
		if ctx.Err() != nil {
			return "", "", false, ctx.Err()
		}
		var status *searchHTTPError
		if errors.As(err, &status) {
			if status.status == 429 {
				t.coolSearchProvider(provider, status.cooldown)
			}
			if status.status == 400 || status.status == 422 {
				return "", "", false, err
			}
		}
		failures = append(failures, provider+": "+err.Error())
	}
	if len(failures) == 0 {
		return "", "", false, fmt.Errorf("domain-filtered search requires EXA_API_KEY or TAVILY_API_KEY; filters are never silently removed")
	}
	return "", "", false, fmt.Errorf("web search unavailable: %s", strings.Join(failures, "; "))
}

func (t *WebSearchTool) searchJSON(ctx context.Context, provider, query string, n int, domains []string, key string) (string, error) {
	endpoint := "https://api.exa.ai/search"
	request := map[string]any{"query": query, "type": "auto", "numResults": n, "contents": map[string]any{"highlights": true}}
	if len(domains) > 0 {
		request["includeDomains"] = domains
	}
	if provider == "tavily" {
		endpoint = "https://api.tavily.com/search"
		request = map[string]any{"query": query, "max_results": n, "search_depth": "basic", "include_answer": false, "auto_parameters": false}
		if len(domains) > 0 {
			request["include_domains"] = domains
			request["include_domains_mode"] = "restrict"
		}
	}
	body, err := json.Marshal(request)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if provider == "exa" {
		req.Header.Set("X-Api-Key", key)
	} else {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := doWebRequest(t.client, req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", &searchHTTPError{provider: provider, status: resp.StatusCode, cooldown: searchCooldown(resp.Header.Get("Retry-After"))}
	}
	body, err = readWebBody(resp.Body, maxSearchBody)
	if err != nil {
		return "", err
	}
	var result struct {
		Results []searchHit `json:"results"`
	}
	if err = json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("%s returned invalid JSON", provider)
	}
	return renderSearchHits(result.Results, n, domains)
}

func renderSearchHits(hits []searchHit, n int, domains []string) (string, error) {
	var b strings.Builder
	seen := map[string]bool{}
	count := 0
	for _, hit := range hits {
		u, err := url.Parse(hit.URL)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil {
			continue
		}
		if len(domains) > 0 {
			allowed := false
			host := strings.ToLower(u.Hostname())
			for _, domain := range domains {
				if host == domain || strings.HasSuffix(host, "."+domain) {
					allowed = true
					break
				}
			}
			if !allowed {
				continue
			}
		}
		u.Fragment = ""
		q := u.Query()
		for name := range q {
			if strings.HasPrefix(strings.ToLower(name), "utm_") || name == "gclid" || name == "fbclid" {
				q.Del(name)
			}
		}
		u.RawQuery = q.Encode()
		canonical := u.String()
		if seen[canonical] {
			continue
		}
		seen[canonical] = true
		count++
		title := strings.NewReplacer("\\", "\\\\", "[", "\\[", "]", "\\]", "\n", " ", "\r", " ").Replace(hit.Title)
		if title == "" {
			title = u.Hostname()
		}
		link := strings.NewReplacer("(", "%28", ")", "%29").Replace(canonical)
		fmt.Fprintf(&b, "%d. [%s](%s)\n", count, title, link)
		if hit.PublishedDate != "" {
			fmt.Fprintf(&b, "Published: %s\n", hit.PublishedDate)
		}
		snippet := strings.Join(hit.Highlights, "\n")
		if snippet == "" {
			snippet = hit.Text
		}
		if snippet == "" {
			snippet = hit.Content
		}
		b.WriteString(snippet + "\n\n")
		if count >= n {
			break
		}
	}
	if count == 0 {
		return "", fmt.Errorf("search returned no usable source URLs matching the requested filters")
	}
	return b.String(), nil
}

func searchCooldown(raw string) time.Duration {
	if seconds, err := time.ParseDuration(raw + "s"); err == nil && seconds > 0 {
		return min(seconds, 10*time.Minute)
	}
	if date, err := http.ParseTime(raw); err == nil && time.Until(date) > 0 {
		return min(time.Until(date), 10*time.Minute)
	}
	return time.Minute
}

func (t *WebSearchTool) searchCoolingDown(provider string) bool {
	t.coolMu.Lock()
	defer t.coolMu.Unlock()
	return time.Now().Before(t.coolUntil[provider])
}
func (t *WebSearchTool) coolSearchProvider(provider string, duration time.Duration) {
	t.coolMu.Lock()
	defer t.coolMu.Unlock()
	if t.coolUntil == nil {
		t.coolUntil = map[string]time.Time{}
	}
	t.coolUntil[provider] = time.Now().Add(duration)
}
