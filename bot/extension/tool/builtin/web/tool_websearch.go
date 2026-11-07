package web

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/toolutil"
	"nekocode/util/text"
)

type WebSearchTool struct {
	toolutil.SafeReadOnlyTool
	client    *http.Client
	coolMu    sync.Mutex
	coolUntil map[string]time.Time
}

func NewWebSearchTool() *WebSearchTool {
	return &WebSearchTool{client: toolutil.NewToolHTTPClient(30 * time.Second)}
}

func (t *WebSearchTool) Name() string { return "web_search" }

func (t *WebSearchTool) Description() string {
	return "Search the public web for source URLs and excerpts, not independently verified facts. auto uses Exa and can fall back to Tavily only when TAVILY_API_KEY is configured; provider selects one service explicitly. EXA_API_KEY enables Exa's structured API; without it Exa's public MCP endpoint is used. Domain filters require a configured structured provider and are never silently dropped. Results disclose provider switching. Filtered/truncated results preserve full retrieved content for read. Open important source pages with web_fetch or web_extract before relying on snippets. Include source links in the answer."
}

func (t *WebSearchTool) Parameters() []core.Parameter {
	return []core.Parameter{
		{Name: "query", Type: "string", Required: true, Description: "Search query"},
		{Name: "numResults", Type: "number", Required: false, Description: "Number of results, default 8, max 15"},
		{Name: "provider", Type: "string", Enum: []string{"auto", "exa", "tavily"}, Description: "Default auto; explicit providers disable cross-provider fallback. Search queries are sent to the selected service(s)."},
		{Name: "domains", Type: "array", Items: &core.Schema{Type: "string"}, Description: "Optional 1–20 DNS domains to restrict sources, including their subdomains. Requires EXA_API_KEY or TAVILY_API_KEY. Omit for unrestricted search."},
	}
}

func (t *WebSearchTool) Execute(ctx context.Context, args map[string]any) (core.ToolOutput, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	options, err := parseSearchOptions(args)
	if err != nil {
		return core.ToolOutput{}, err
	}
	query, err := toolutil.RequireStringArg(args, "query")
	if err != nil {
		return core.ToolOutput{}, err
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return core.ToolOutput{}, fmt.Errorf("query must not be blank")
	}
	n := 8
	if raw, present := args["numResults"]; present {
		var value float64
		switch v := raw.(type) {
		case float64:
			value = v
		case int:
			value = float64(v)
		default:
			return core.ToolOutput{}, fmt.Errorf("numResults must be a positive integer")
		}
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 1 || math.Trunc(value) != value {
			return core.ToolOutput{}, fmt.Errorf("numResults must be a positive integer")
		}
		// Clamp before conversion so enormous JSON numbers cannot overflow int.
		n = int(math.Min(value, 15))
	}
	content, method, switched, err := t.searchProviders(ctx, query, n, options)
	if err != nil {
		return core.ToolOutput{}, err
	}
	if switched {
		content = "[Search provider switched after an unavailable or rate-limited provider.]\n\n" + content
	}
	return webResult(ctx, "", method, content, "", 12000, false)
}

// --- Exa MCP (JSON-RPC over SSE) ---

func exaEndpoint() string {
	return "https://mcp.exa.ai/mcp"
}

func (t *WebSearchTool) searchExa(ctx context.Context, query string, n int) (string, error) {
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "web_search_exa",
			"arguments": map[string]any{
				"query":                query,
				"numResults":           n,
				"livecrawl":            "fallback",
				"type":                 "auto",
				"contextMaxCharacters": 10000,
			},
		},
	})
	if err != nil {
		return "", fmt.Errorf("encode search request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, exaEndpoint(), bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build search request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if k := os.Getenv("EXA_API_KEY"); k != "" {
		req.Header.Set("X-Api-Key", k)
	}

	resp, err := doWebRequest(t.client, req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", &searchHTTPError{provider: "exa", status: resp.StatusCode, cooldown: searchCooldown(resp.Header.Get("Retry-After"))}
	}
	if strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "application/json") {
		body, err := readWebBody(resp.Body, maxSearchBody)
		if err != nil {
			return "", err
		}
		return parseExaResult(body)
	}
	return parseExaSSE(resp.Body)
}

const maxSearchBody = 2 << 20

func parseExaResult(data []byte) (string, error) {
	var v struct {
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		Result struct {
			IsError bool `json:"isError"`
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal(data, &v); err != nil {
		return "", fmt.Errorf("invalid search response: %w", err)
	}
	if v.Error != nil {
		return "", fmt.Errorf("exa RPC error %d: %s", v.Error.Code, text.TruncateByRune(v.Error.Message, 1000))
	}
	var blocks []string
	for _, block := range v.Result.Content {
		if strings.TrimSpace(block.Text) != "" {
			blocks = append(blocks, block.Text)
		}
	}
	content := strings.Join(blocks, "\n\n")
	if v.Result.IsError {
		return "", fmt.Errorf("exa search failed: %s", text.TruncateByRune(content, 1000))
	}
	if content == "" {
		return "", fmt.Errorf("exa returned no search content")
	}
	return content, nil
}

func parseExaSSE(r io.Reader) (string, error) {
	limited := &io.LimitedReader{R: r, N: maxSearchBody + 1}
	scan := bufio.NewScanner(limited)
	scan.Buffer(make([]byte, 4096), maxSearchBody+1)
	var data []string
	parseEvent := func() (string, error) {
		payload := strings.Join(data, "\n")
		data = nil
		// Notifications and keepalives may precede the response.
		var envelope map[string]json.RawMessage
		if err := json.Unmarshal([]byte(payload), &envelope); err != nil {
			return "", fmt.Errorf("invalid search event: %w", err)
		}
		if envelope["result"] == nil && envelope["error"] == nil {
			return "", nil
		}
		return parseExaResult([]byte(payload))
	}
	for scan.Scan() {
		line := scan.Text()
		if line == "" && len(data) > 0 {
			if result, err := parseEvent(); result != "" || err != nil {
				return result, err
			}
		}
		if strings.HasPrefix(line, "data:") {
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	if limited.N == 0 {
		return "", fmt.Errorf("search response exceeds size limit")
	}
	if err := scan.Err(); err != nil {
		return "", fmt.Errorf("read search stream: %w", err)
	}
	if len(data) > 0 {
		if result, err := parseEvent(); result != "" || err != nil {
			return result, err
		}
	}
	return "", errors.New("search stream ended without a result")
}
