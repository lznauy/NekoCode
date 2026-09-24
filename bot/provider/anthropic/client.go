package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"nekocode/bot/provider/types"
	"nekocode/logger"
	"nekocode/util/url"
)

type Client struct {
	types.BaseClient
}

func New(apiKey, baseURL, model string) *Client {
	if baseURL == "" {
		baseURL = "https://api.anthropic.com/v1"
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

// --- request/response types ---

type contentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	Thinking  *string         `json:"thinking,omitempty"`
	Signature string          `json:"signature,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   string          `json:"content,omitempty"`
}

type tool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema any    `json:"input_schema"`
}

type message struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type request struct {
	Model        string          `json:"model"`
	MaxTokens    int             `json:"max_tokens"`
	Temperature  float64         `json:"temperature,omitempty"`
	System       string          `json:"system,omitempty"`
	Messages     []message       `json:"messages"`
	Tools        []tool          `json:"tools,omitempty"`
	Stream       bool            `json:"stream"`
	OutputConfig *outputConfig   `json:"output_config,omitempty"`
	Thinking     *thinkingConfig `json:"thinking,omitempty"`
}

type outputConfig struct {
	Effort string `json:"effort"`
}

type thinkingConfig struct {
	Type string `json:"type"`
}

type response struct {
	ID      string         `json:"id"`
	Content []contentBlock `json:"content"`
	Usage   wireUsage      `json:"usage"`
}

type wireUsage struct {
	InputTokens              int  `json:"input_tokens"`
	OutputTokens             int  `json:"output_tokens"`
	CacheCreationInputTokens *int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     *int `json:"cache_read_input_tokens"`
}

type sseEvent struct {
	Type         string          `json:"type"`
	Index        int             `json:"index"`
	Delta        json.RawMessage `json:"delta"`
	ContentBlock json.RawMessage `json:"content_block"`
	Usage        *wireUsage      `json:"usage"`
	Message      struct {
		Usage wireUsage `json:"usage"`
	} `json:"message"`
}

type textDelta struct {
	Type      string `json:"type"`
	Text      string `json:"text"`
	Thinking  string `json:"thinking"`
	Signature string `json:"signature"`
}

type inputJSONDelta struct {
	Type        string `json:"type"`
	PartialJSON string `json:"partial_json"`
}

// --- conversion ---

func toTools(tools []types.ToolDef) []tool {
	if len(tools) == 0 {
		return nil
	}
	out := make([]tool, len(tools))
	for i, t := range tools {
		out[i] = tool{
			Name:        t.Function.Name,
			Description: t.Function.Description,
			InputSchema: t.Function.Parameters,
		}
	}
	return out
}

func toMessages(messages []types.Message, reasoning types.ReasoningSettings) ([]message, string) {
	var systemPrompt string
	var out []message

	for _, msg := range messages {
		if msg.Role == "system" {
			if msg.Source == types.MessageSourceVolatileTail {
				out = append(out, message{Role: "user", Content: msg.Content})
				continue
			}
			if systemPrompt != "" {
				systemPrompt += "\n\n"
			}
			systemPrompt += msg.Content
			continue
		}
		if msg.Role == "tool" {
			out = append(out, message{
				Role: "user",
				Content: []contentBlock{{
					Type:      "tool_result",
					ToolUseID: msg.ToolCallID,
					Content:   msg.Content,
				}},
			})
			continue
		}
		if msg.Role == "assistant" && (len(msg.ToolCalls) > 0 || msg.ReasoningSignature != "") {
			blocks := make([]contentBlock, 0, len(msg.ToolCalls)+2)
			if content, replay := types.ReasoningForRequest(msg, reasoning); replay {
				blocks = append(blocks, contentBlock{Type: "thinking", Thinking: &content, Signature: msg.ReasoningSignature})
			}
			if msg.Content != "" {
				blocks = append(blocks, contentBlock{Type: "text", Text: msg.Content})
			}
			for _, tc := range msg.ToolCalls {
				blocks = append(blocks, contentBlock{
					Type:  "tool_use",
					ID:    tc.ID,
					Name:  tc.Function.Name,
					Input: json.RawMessage(tc.Function.Arguments),
				})
			}
			out = append(out, message{Role: msg.Role, Content: blocks})
			continue
		}
		out = append(out, message{Role: msg.Role, Content: msg.Content})
	}
	return out, systemPrompt
}

func toResponse(ar *response) *types.Response {
	resp := &types.Response{ID: ar.ID}
	var text string
	var reasoning string
	var signature string
	var toolCalls []types.ToolCall
	for _, block := range ar.Content {
		switch block.Type {
		case "text":
			text += block.Text
		case "thinking":
			if block.Thinking != nil {
				reasoning += *block.Thinking
			}
			signature = block.Signature
		case "tool_use":
			toolCalls = append(toolCalls, types.ToolCall{
				ID:   block.ID,
				Type: "function",
				Function: types.FunctionCall{
					Name:      block.Name,
					Arguments: string(block.Input),
				},
			})
		}
	}
	resp.Choices = []types.Choice{{
		Message: types.Message{Role: "assistant", Content: text, ReasoningContent: reasoning,
			ReasoningSignature: signature, ToolCalls: toolCalls},
	}}
	resp.Usage = normalizeUsage(ar.Usage)
	return resp
}

func normalizeUsage(usage wireUsage) types.StreamUsage {
	created := intValue(usage.CacheCreationInputTokens)
	read := intValue(usage.CacheReadInputTokens)
	cacheReported := usage.CacheCreationInputTokens != nil || usage.CacheReadInputTokens != nil
	miss := 0
	if cacheReported {
		miss = usage.InputTokens + created
	}
	return types.StreamUsage{
		PromptTokens:       usage.InputTokens + created + read,
		CompletionTokens:   usage.OutputTokens,
		TotalTokens:        usage.InputTokens + created + read + usage.OutputTokens,
		CacheHitTokens:     read,
		CacheMissTokens:    miss,
		CacheUsageReported: cacheReported,
	}
}

func intValue(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

// --- public ---

func (c *Client) buildRequest(messages []types.Message, tools []types.ToolDef, stream bool) *request {
	reasoning := c.ReasoningSettings()
	msgs, sys := toMessages(messages, reasoning)
	req := &request{
		Model:       c.Model,
		MaxTokens:   c.GetMaxTokens(),
		Temperature: c.Temperature,
		System:      sys,
		Messages:    msgs,
		Tools:       toTools(tools),
		Stream:      stream,
	}
	if !reasoning.Disabled && reasoning.Effort != "" {
		req.OutputConfig = &outputConfig{Effort: reasoning.Effort}
		if reasoning.ThinkingMode != "" {
			req.Thinking = &thinkingConfig{Type: reasoning.ThinkingMode}
		}
	}
	return req
}

func (c *Client) headers() map[string]string {
	return map[string]string{
		"x-api-key":         c.APIKey,
		"Authorization":     "Bearer " + c.APIKey,
		"anthropic-version": "2023-06-01",
	}
}

func (c *Client) RequestMeta() types.RequestMeta {
	reasoning := c.ReasoningSettings()
	return types.RequestMeta{Model: c.Model, Protocol: "anthropic", BaseURL: c.BaseURL,
		RequestedEffort: reasoning.RequestedValue(), EffectiveEffort: reasoning.EffectiveValue()}
}

func (c *Client) endpoint(path string) string {
	return url.JoinURLPathWithVersion(c.BaseURL, "v1", path)
}

// newStreamRequest creates an *http.Request for streaming, reusing pre-marshaled body.
func (c *Client) newStreamRequest(ctx context.Context, jsonBody []byte) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint("messages"), bytes.NewBuffer(jsonBody))
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
	body := c.buildRequest(messages, tools, false)
	data, err := types.DoJSONRequest(ctx, c.endpoint("messages"), c.headers(), body)
	if err != nil {
		return nil, err
	}
	var ar response
	if err := json.Unmarshal(data, &ar); err != nil {
		return nil, err
	}
	return toResponse(&ar), nil
}

func (c *Client) ChatStream(ctx context.Context, messages []types.Message, tools []types.ToolDef) (<-chan types.StreamToken, <-chan error) {
	tokenCh := make(chan types.StreamToken)
	errCh := make(chan error, 1)

	go func() {
		defer close(tokenCh)
		defer close(errCh)

		body := c.buildRequest(messages, tools, true)
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

		type toolAccum struct {
			id   string
			name string
			args strings.Builder
		}
		toolAccums := make(map[int]*toolAccum)

		types.StreamSSE(ctx, resp, tokenCh, errCh, func(data string, tokenCh chan<- types.StreamToken) error {
			var event sseEvent
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				// Keep-alives and non-event payloads are ignored, but leave a
				// diagnosis trail: a stream of only such payloads yields an
				// empty response with no other error.
				logger.Log("anthropic stream: ignored unparseable SSE payload (%d bytes)", len(data))
				return nil
			}

			switch event.Type {
			case "message_stop":
				return types.ErrStreamDone
			case "message_start":
				usage := event.Message.Usage
				if usage.InputTokens > 0 || usage.CacheCreationInputTokens != nil || usage.CacheReadInputTokens != nil {
					u := normalizeUsage(usage)
					tokenCh <- types.StreamToken{Usage: &u}
				}
			case "content_block_start":
				var cb contentBlock
				if err := json.Unmarshal(event.ContentBlock, &cb); err != nil {
					return nil
				}
				if cb.Type == "tool_use" {
					toolAccums[event.Index] = &toolAccum{id: cb.ID, name: cb.Name}
				} else if cb.Type == "thinking" {
					if cb.Thinking != nil && *cb.Thinking != "" {
						tokenCh <- types.StreamToken{ReasoningContent: *cb.Thinking}
					}
					if cb.Signature != "" {
						tokenCh <- types.StreamToken{ReasoningSignature: cb.Signature}
					}
				}
			case "content_block_delta":
				var td textDelta
				if json.Unmarshal(event.Delta, &td) == nil {
					switch td.Type {
					case "text_delta":
						tokenCh <- types.StreamToken{Content: td.Text}
						return nil
					case "thinking_delta":
						tokenCh <- types.StreamToken{ReasoningContent: td.Thinking}
						return nil
					case "signature_delta":
						tokenCh <- types.StreamToken{ReasoningSignature: td.Signature}
						return nil
					}
				}
				var ijd inputJSONDelta
				if json.Unmarshal(event.Delta, &ijd) == nil && ijd.Type == "input_json_delta" {
					if acc := toolAccums[event.Index]; acc != nil {
						acc.args.WriteString(ijd.PartialJSON)
						tokenCh <- types.StreamToken{
							ToolCallDelta: &types.ToolCallDelta{
								Index: event.Index, ID: acc.id, Name: acc.name,
								Arguments: ijd.PartialJSON,
							},
						}
					}
				}
			case "message_delta":
				if event.Usage != nil && event.Usage.OutputTokens > 0 {
					tokenCh <- types.StreamToken{
						Usage: &types.StreamUsage{CompletionTokens: event.Usage.OutputTokens},
					}
				}
			}
			return nil
		})
	}()

	return tokenCh, errCh
}
