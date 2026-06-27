package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"

	"nekocode/llm/types"
)

type streamChunk struct {
	Choices []struct {
		Delta        delta  `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *types.StreamUsage `json:"usage"`
}

type delta struct {
	Content          string           `json:"content"`
	ReasoningContent string           `json:"reasoning_content"`
	ToolCalls        []types.ToolCall `json:"tool_calls,omitempty"`
}

type apiMessage struct {
	Role             string           `json:"role"`
	Content          string           `json:"content,omitempty"`
	ReasoningContent string           `json:"reasoning_content,omitempty"`
	Name             string           `json:"name,omitempty"`
	ToolCalls        []types.ToolCall `json:"tool_calls,omitempty"`
	ToolCallID       string           `json:"tool_call_id,omitempty"`
}

type Client struct {
	types.BaseClient
	reasoningEffort string
}

func New(apiKey, baseURL, model string) *Client {
	if baseURL == "" {
		baseURL = "https://api.deepseek.com/v1"
	}
	return &Client{
		BaseClient: types.BaseClient{
			APIKey:      apiKey,
			BaseURL:     baseURL,
			Model:       model,
			MaxTokens:   32000,
			Temperature: 0.3,
		},
	}
}

func (c *Client) headers() map[string]string {
	return map[string]string{
		"Authorization": "Bearer " + c.APIKey,
	}
}

// newStreamRequest creates an *http.Request for streaming, reusing pre-marshaled body.
func (c *Client) newStreamRequest(ctx context.Context, jsonBody []byte) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/chat/completions", bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range c.headers() {
		req.Header.Set(k, v)
	}
	return req, nil
}

func (c *Client) Chat(ctx context.Context, messages []types.Message, tools []types.ToolDef) (*types.Response, error) {
	body := c.buildBody(messages, tools, false)
	data, err := types.DoJSONRequest(ctx, c.BaseURL+"/chat/completions", c.headers(), body)
	if err != nil {
		return nil, err
	}
	var r types.Response
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

func (c *Client) ChatStream(ctx context.Context, messages []types.Message, tools []types.ToolDef) (<-chan types.StreamToken, <-chan error) {
	tokenCh := make(chan types.StreamToken)
	errCh := make(chan error, 1)

	go func() {
		defer close(tokenCh)
		defer close(errCh)

		body := c.buildBody(messages, tools, true)
		jsonBody, _ := json.Marshal(body)

		req, err := c.newStreamRequest(ctx, jsonBody)
		if err != nil {
			errCh <- err
			return
		}

		resp, err := types.SharedHTTPStreamClient.Do(req)
		if err != nil {
			errCh <- err
			return
		}

		types.StreamSSE(ctx, resp, tokenCh, errCh, func(data string, tokenCh chan<- types.StreamToken) error {
			var chunk streamChunk
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				return nil
			}
			if chunk.Usage != nil {
				chunk.Usage.Normalize()
			}
			if len(chunk.Choices) == 0 {
				// Mimo sends usage in a separate final chunk with empty choices.
				if chunk.Usage != nil {
					tokenCh <- types.StreamToken{Usage: chunk.Usage}
				}
				return nil
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
			return nil
		})
	}()

	return tokenCh, errCh
}

func (c *Client) buildBody(messages []types.Message, tools []types.ToolDef, stream bool) map[string]any {
	body := map[string]any{
		"model": c.Model, "messages": toAPIMessages(messages),
		"max_tokens": c.GetMaxTokens(), "temperature": c.Temperature,
		"stream": stream,
	}
	if len(tools) > 0 {
		body["tools"] = tools
		body["tool_choice"] = "auto"
	}
	if c.GetDisableThinking() {
		body["thinking"] = map[string]string{"type": "disabled"}
	} else if c.reasoningEffort != "" {
		body["reasoning_effort"] = c.reasoningEffort
		body["thinking"] = map[string]string{"type": "enabled"}
	}
	return body
}

func toAPIMessages(messages []types.Message) []apiMessage {
	out := make([]apiMessage, 0, len(messages))
	for _, m := range messages {
		out = append(out, apiMessage{
			Role:             m.Role,
			Content:          m.Content,
			ReasoningContent: m.ReasoningContent,
			Name:             m.Name,
			ToolCalls:        m.ToolCalls,
			ToolCallID:       m.ToolCallID,
		})
	}
	return out
}
