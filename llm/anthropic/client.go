package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"nekocode/llm/types"
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
			APIKey:      apiKey,
			BaseURL:     baseURL,
			Model:       model,
			MaxTokens:   32000,
			Temperature: 0.7,
		},
	}
}

// --- request/response types ---

type contentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
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
	Model       string    `json:"model"`
	MaxTokens   int       `json:"max_tokens"`
	Temperature float64   `json:"temperature"`
	System      string    `json:"system,omitempty"`
	Messages    []message `json:"messages"`
	Tools       []tool    `json:"tools,omitempty"`
	Stream      bool      `json:"stream"`
}

type response struct {
	ID      string         `json:"id"`
	Content []contentBlock `json:"content"`
	Usage   struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

type sseEvent struct {
	Type         string          `json:"type"`
	Index        int             `json:"index"`
	Delta        json.RawMessage `json:"delta"`
	ContentBlock json.RawMessage `json:"content_block"`
	Usage        *struct {
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	Message struct {
		Usage struct {
			InputTokens          int `json:"input_tokens"`
			CacheReadInputTokens int `json:"cache_read_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

type textDelta struct {
	Type string `json:"type"`
	Text string `json:"text"`
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

func toMessages(messages []types.Message) ([]message, string) {
	var systemPrompt string
	var out []message

	for _, msg := range messages {
		if msg.Role == "system" {
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
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			blocks := make([]contentBlock, 0, len(msg.ToolCalls)+1)
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
	var toolCalls []types.ToolCall
	for _, block := range ar.Content {
		switch block.Type {
		case "text":
			text += block.Text
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
		Message: types.Message{Role: "assistant", Content: text, ToolCalls: toolCalls},
	}}
	resp.Usage.PromptTokens = ar.Usage.InputTokens
	resp.Usage.CompletionTokens = ar.Usage.OutputTokens
	resp.Usage.TotalTokens = ar.Usage.InputTokens + ar.Usage.OutputTokens
	return resp
}

// --- public ---

func (c *Client) buildRequest(messages []types.Message, tools []types.ToolDef, stream bool) *request {
	msgs, sys := toMessages(messages)
	return &request{
		Model:       c.Model,
		MaxTokens:   c.GetMaxTokens(),
		Temperature: c.Temperature,
		System:      sys,
		Messages:    msgs,
		Tools:       toTools(tools),
		Stream:      stream,
	}
}

func (c *Client) headers() map[string]string {
	return map[string]string{
		"x-api-key":         c.APIKey,
		"anthropic-version": "2023-06-01",
	}
}

// newStreamRequest creates an *http.Request for streaming, reusing pre-marshaled body.
func (c *Client) newStreamRequest(ctx context.Context, jsonBody []byte) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/messages", bytes.NewBuffer(jsonBody))
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
	data, err := types.DoJSONRequest(ctx, c.BaseURL+"/messages", c.headers(), body)
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
				return nil
			}

			switch event.Type {
			case "message_start":
				if event.Message.Usage.InputTokens > 0 || event.Message.Usage.CacheReadInputTokens > 0 {
					u := &types.StreamUsage{
						PromptTokens:   event.Message.Usage.InputTokens,
						CacheHitTokens: event.Message.Usage.CacheReadInputTokens,
					}
					u.Normalize()
					tokenCh <- types.StreamToken{Usage: u}
				}
			case "content_block_start":
				var cb contentBlock
				if err := json.Unmarshal(event.ContentBlock, &cb); err != nil {
					return nil
				}
				if cb.Type == "tool_use" {
					toolAccums[event.Index] = &toolAccum{id: cb.ID, name: cb.Name}
				}
			case "content_block_delta":
				var td textDelta
				if json.Unmarshal(event.Delta, &td) == nil && td.Type == "text_delta" {
					tokenCh <- types.StreamToken{Content: td.Text}
					return nil
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
