package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"

	"nekocode/bot/provider/types"
	"nekocode/bot/reasoning"
	"nekocode/logger"
	"nekocode/util/url"
)

type streamChunk struct {
	Choices []struct {
		Delta        delta  `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage             *types.StreamUsage `json:"usage"`
	SystemFingerprint string             `json:"system_fingerprint"`
}

type delta struct {
	Content          string           `json:"content"`
	ReasoningContent string           `json:"reasoning_content"`
	ToolCalls        []types.ToolCall `json:"tool_calls,omitempty"`
}

type apiMessage struct {
	Role             string           `json:"role"`
	Content          string           `json:"content"`
	ReasoningContent *string          `json:"reasoning_content,omitempty"`
	Name             string           `json:"name,omitempty"`
	ToolCalls        []types.ToolCall `json:"tool_calls,omitempty"`
	ToolCallID       string           `json:"tool_call_id,omitempty"`
}

type Client struct {
	types.BaseClient
}

func New(apiKey, baseURL, model string) *Client {
	if baseURL == "" {
		baseURL = "https://api.deepseek.com/v1"
	}
	return &Client{
		BaseClient: types.BaseClient{
			APIKey:    apiKey,
			BaseURL:   baseURL,
			Model:     model,
			MaxTokens: 32768,
		},
	}
}

func (c *Client) headers() map[string]string {
	return map[string]string{
		"Authorization": "Bearer " + c.APIKey,
	}
}

func (c *Client) RequestMeta() types.RequestMeta {
	reasoning := c.ReasoningSettings()
	return types.RequestMeta{Model: c.Model, Protocol: "openai", BaseURL: c.BaseURL,
		RequestedEffort: reasoning.RequestedValue(), EffectiveEffort: reasoning.EffectiveValue()}
}

func (c *Client) endpoint(path string) string {
	return url.JoinURLPathWithVersion(c.BaseURL, "v1", path)
}

// newStreamRequest creates an *http.Request for streaming, reusing pre-marshaled body.
func (c *Client) newStreamRequest(ctx context.Context, jsonBody []byte) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint("chat/completions"), bytes.NewBuffer(jsonBody))
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
	data, err := types.DoJSONRequest(ctx, c.endpoint("chat/completions"), c.headers(), body)
	if err != nil {
		return nil, err
	}
	var r types.Response
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, err
	}
	r.Usage.Normalize()
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
		meta := c.RequestMeta()
		tokenCh <- types.StreamToken{Request: &meta}
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
				// Providers send keep-alives and occasional non-chunk payloads;
				// ignore unparseable data but leave a diagnosis trail, since a
				// stream of only such payloads yields an empty response.
				logger.Log("openai stream: ignored unparseable SSE payload (%d bytes)", len(data))
				return nil
			}
			if chunk.Usage != nil {
				chunk.Usage.SystemFingerprint = chunk.SystemFingerprint
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
	settings := c.ReasoningSettings()
	body := map[string]any{
		"model": c.Model, "messages": toAPIMessages(messages, settings),
		"max_tokens": c.GetMaxTokens(), "stream": stream,
	}
	if c.Temperature != 0 {
		body["temperature"] = c.Temperature
	}
	if len(tools) > 0 {
		body["tools"] = tools
		if settings.Replay != reasoning.ReplayToolCalls {
			body["tool_choice"] = "auto"
		}
	}
	if settings.Disabled {
		if settings.DisableEffort != "" {
			body["reasoning_effort"] = settings.DisableEffort
		}
		if settings.ThinkingMode != "" {
			body["thinking"] = map[string]string{"type": "disabled"}
		}
	} else if settings.Effort != "" {
		body["reasoning_effort"] = settings.Effort
		if settings.ThinkingMode != "" {
			body["thinking"] = map[string]string{"type": settings.ThinkingMode}
		}
	}
	return body
}

// toAPIMessages converts internal messages to the wire format, preserving
// their order and positions. Dynamic controller state is already represented
// as tagged user messages in the append-only history; moving or hoisting any
// message here would invalidate the provider's cached prefix.
func toAPIMessages(messages []types.Message, reasoning types.ReasoningSettings) []apiMessage {
	out := make([]apiMessage, 0, len(messages))
	for _, m := range messages {
		if m.Role == "system" && m.Content == "" {
			continue
		}
		wire := apiMessage{
			Role:       m.Role,
			Content:    m.Content,
			Name:       m.Name,
			ToolCalls:  m.ToolCalls,
			ToolCallID: m.ToolCallID,
		}
		if content, replay := types.ReasoningForRequest(m, reasoning); replay {
			wire.ReasoningContent = &content
		}
		out = append(out, wire)
	}
	return out
}
