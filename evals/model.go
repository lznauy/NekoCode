package evals

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// This client is local to the evaluation module. It does not load application
// configuration, credentials, tool registries, or runtime state.
type chatModel interface {
	Chat(context.Context, []message, []toolDef) (*modelResponse, error)
}

type modelConfig struct {
	Model, BaseURL, APIKey string
	MaxTokens              int
}
type modelClient struct {
	config modelConfig
	http   *http.Client
}

func newModel(config modelConfig) chatModel {
	return &modelClient{config: config, http: &http.Client{Timeout: 90 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

func (c *modelClient) Chat(ctx context.Context, messages []message, tools []toolDef) (*modelResponse, error) {
	data, err := json.Marshal(struct {
		Model     string    `json:"model"`
		Messages  []message `json:"messages"`
		Tools     []toolDef `json:"tools"`
		MaxTokens int       `json:"max_tokens"`
	}{c.config.Model, messages, tools, c.config.MaxTokens})
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(c.config.BaseURL, "/")+"/chat/completions", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+c.config.APIKey)
	request.Header.Set("Content-Type", "application/json")
	response, err := c.http.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("model endpoint returned HTTP %d", response.StatusCode)
	}
	const maxResponse = 2 << 20
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponse+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxResponse {
		return nil, fmt.Errorf("model response exceeds size limit")
	}
	var result modelResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

type message struct {
	Role             string     `json:"role"`
	Content          string     `json:"content,omitempty"`
	ReasoningContent string     `json:"reasoning_content,omitempty"`
	ToolCalls        []toolCall `json:"tool_calls,omitempty"`
	ToolCallID       string     `json:"tool_call_id,omitempty"`
	IsError          bool       `json:"-"`
}
type toolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function functionCall `json:"function"`
}
type functionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}
type choice struct {
	Message message `json:"message"`
}
type modelResponse struct {
	Choices []choice `json:"choices"`
}
type toolDef struct {
	Type     string      `json:"type"`
	Function functionDef `json:"function"`
}
type functionDef struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Parameters  parameters `json:"parameters"`
}
type parameters struct {
	Type       string              `json:"type"`
	Properties map[string]property `json:"properties"`
	Required   []string            `json:"required"`
}
type property struct {
	Type string `json:"type"`
}
