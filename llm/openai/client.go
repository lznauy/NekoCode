package openai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"nekocode/llm/types"
)

type streamChunk struct {
	Choices []struct {
		Delta        delta         `json:"delta"`
		FinishReason string        `json:"finish_reason"`
	} `json:"choices"`
	Usage *types.StreamUsage `json:"usage"`
}

type delta struct {
	Content          string        `json:"content"`
	ReasoningContent string        `json:"reasoning_content"`
	ToolCalls        []types.ToolCall `json:"tool_calls,omitempty"`
}

type Client struct {
	APIKey          string
	BaseURL         string
	Model           string
	maxTokens       int
	temperature     float64
	disableThinking bool
	reasoningEffort string
}

func New(apiKey, baseURL, model string) *Client {
	if baseURL == "" {
		baseURL = "https://api.deepseek.com/v1"
	}
	return &Client{
		APIKey:      apiKey,
		BaseURL:     baseURL,
		Model:       model,
		maxTokens:   32000,
		temperature: 0.3,
	}
}

func (c *Client) SetMaxTokens(n int)        { c.maxTokens = n }
func (c *Client) SetDisableThinking(d bool) { c.disableThinking = d }

func (c *Client) Chat(ctx context.Context, messages []types.Message, tools []types.ToolDef) (*types.Response, error) {
	body := c.buildBody(messages, tools, false)
	jsonBody, _ := json.Marshal(body)

	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/chat/completions", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)

	resp, err := types.SharedHTTPClientTimeout.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error (HTTP %d): %s", resp.StatusCode, string(data))
	}

	var r types.Response
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

func (c *Client) ChatStream(ctx context.Context, messages []types.Message, tools []types.ToolDef) (<-chan types.StreamToken, <-chan error) {
	body := c.buildBody(messages, tools, true)
	jsonBody, _ := json.Marshal(body)

	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/chat/completions", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)

	resp, err := types.SharedHTTPStreamClient.Do(req)
	if err != nil {
		ch := make(chan types.StreamToken)
		ech := make(chan error, 1)
		ech <- err
		close(ch)
		return ch, ech
	}

	tokenCh := make(chan types.StreamToken)
	errCh := make(chan error, 1)

	go func() {
		defer close(tokenCh)
		defer close(errCh)
		defer resp.Body.Close()

		done := make(chan struct{})
		go func() {
			select {
			case <-ctx.Done():
				resp.Body.Close()
			case <-done:
			}
		}()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			errCh <- fmt.Errorf("API error (HTTP %d): %s", resp.StatusCode, string(body))
			return
		}

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				continue
			}

			var chunk streamChunk
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				continue
			}
			if len(chunk.Choices) == 0 {
				continue
			}
			delta := chunk.Choices[0].Delta
			token := types.StreamToken{
				Content:          delta.Content,
				ReasoningContent: delta.ReasoningContent,
				Usage:            chunk.Usage,
				FinishReason:     chunk.Choices[0].FinishReason,
			}
			if token.Content != "" || token.ReasoningContent != "" || token.Usage != nil || token.FinishReason != "" {
				tokenCh <- token
			}
			for _, tc := range delta.ToolCalls {
				tokenCh <- types.StreamToken{
					ToolCallDelta: &types.ToolCallDelta{
						Index: tc.Index, ID: tc.ID, Name: tc.Function.Name, Arguments: tc.Function.Arguments,
					},
				}
			}
		}
		close(done)
		if err := scanner.Err(); err != nil {
			if ctx.Err() != nil {
				errCh <- ctx.Err()
			} else {
				errCh <- err
			}
		}
	}()

	return tokenCh, errCh
}

func (c *Client) buildBody(messages []types.Message, tools []types.ToolDef, stream bool) map[string]any {
	body := map[string]any{
		"model": c.Model, "messages": messages,
		"max_tokens": c.maxTokens, "temperature": c.temperature,
		"stream": stream,
	}
	if len(tools) > 0 {
		body["tools"] = tools
		body["tool_choice"] = "auto"
	}
	if c.disableThinking {
		body["thinking"] = map[string]string{"type": "disabled"}
	} else if c.reasoningEffort != "" {
		body["reasoning_effort"] = c.reasoningEffort
		body["thinking"] = map[string]string{"type": "enabled"}
	}
	return body
}
