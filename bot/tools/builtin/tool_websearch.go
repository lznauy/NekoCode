package builtin

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"nekocode/bot/tools"
	"net/http"
	"os"
	"strings"
	"time"

	"nekocode/common"
)

type WebSearchTool struct {
	SafeReadOnlyTool
	client *http.Client
}

func NewWebSearchTool() *WebSearchTool {
	return &WebSearchTool{client: tools.NewToolHTTPClient(30 * time.Second)}
}

func (t *WebSearchTool) Name() string { return "web_search" }

func (t *WebSearchTool) Description() string {
	return "Search the web. Include a \"Sources:\" section with [Title](URL) links after answering."
}

func (t *WebSearchTool) Parameters() []tools.Parameter {
	return []tools.Parameter{
		{Name: "query", Type: "string", Required: true, Description: "Search query"},
		{Name: "numResults", Type: "number", Required: false, Description: "Number of results, default 8, max 15"},
	}
}

func (t *WebSearchTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	query, err := requireStringArg(args, "query")
	if err != nil {
		return "", err
	}
	n := 8
	if v, ok := args["numResults"].(float64); ok && v > 0 {
		n = int(v)
		if n > 15 {
			n = 15
		}
	}
	return t.searchExa(ctx, query, n)
}

// --- Exa MCP (JSON-RPC over SSE) ---

func exaEndpoint() string {
	return "https://mcp.exa.ai/mcp"
}

func (t *WebSearchTool) searchExa(ctx context.Context, query string, n int) (string, error) {
	body, _ := json.Marshal(map[string]any{
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

	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, exaEndpoint(), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if k := os.Getenv("EXA_API_KEY"); k != "" {
		req.Header.Set("X-Api-Key", k)
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("exa: HTTP %d — %s", resp.StatusCode, string(b))
	}
	return parseExaSSE(resp.Body)
}

func parseExaSSE(r io.Reader) (string, error) {
	scan := bufio.NewScanner(r)
	for scan.Scan() {
		line := scan.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var v struct {
			Result struct {
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"result"`
		}
		if json.Unmarshal([]byte(line[6:]), &v) != nil {
			continue
		}
		if len(v.Result.Content) > 0 && v.Result.Content[0].Text != "" {
			return common.TruncateByRune(v.Result.Content[0].Text, 6000), nil
		}
	}
	return "", scan.Err()
}
