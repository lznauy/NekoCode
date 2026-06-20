package mcp

import (
	"encoding/json"
	"fmt"
)

// ListTools returns cached tools after starting the server.
func (c *Client) ListTools() ([]ToolDef, error) {
	if err := c.Start(); err != nil {
		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	result, err := c.sendRequest("tools/list", nil)
	if err != nil {
		return nil, fmt.Errorf("tools/list: %w", err)
	}

	var listResp struct {
		Tools []ToolDef `json:"tools"`
	}
	if err := json.Unmarshal(result, &listResp); err != nil {
		return nil, fmt.Errorf("parse tools/list: %w", err)
	}

	c.tools = listResp.Tools
	return c.tools, nil
}

// CallTool invokes a tool on the MCP server.
func (c *Client) CallTool(name string, args map[string]any) (string, error) {
	if err := c.Start(); err != nil {
		return "", err
	}

	params := map[string]any{
		"name":      name,
		"arguments": args,
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	result, err := c.sendRequest("tools/call", params)
	if err != nil {
		return "", fmt.Errorf("tools/call %s: %w", name, err)
	}

	text, isError, err := parseToolCallResult(result)
	if err != nil {
		return "", err
	}
	if isError {
		return text, fmt.Errorf("tool error: %s", text)
	}
	return text, nil
}

func parseToolCallResult(result []byte) (string, bool, error) {
	var callResp struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(result, &callResp); err != nil {
		return "", false, fmt.Errorf("parse tools/call: %w", err)
	}

	var text string
	for _, item := range callResp.Content {
		if item.Type == "text" {
			text += item.Text
		}
	}
	return text, callResp.IsError, nil
}
