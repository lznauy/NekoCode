package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type Anthropic struct {
	APIKey          string
	BaseURL         string
	Model           string
	maxTokens       int
	thinkingType    string // "adaptive" (default), "enabled", "disabled"
	thinkingBudget  int    // only used when thinkingType == "enabled"
	temperature     float64
}

func NewAnthropic(apiKey, baseURL, model string) *Anthropic {
	if baseURL == "" {
		baseURL = "https://api.anthropic.com/v1"
	}
	return &Anthropic{
		APIKey:       apiKey,
		BaseURL:      baseURL,
		Model:        model,
		maxTokens:    32000,
		temperature:  0.7,
		thinkingType: "adaptive", // default thinking mode — safe for DeepSeek too
	}
}

func (a *Anthropic) SetAPIKey(apiKey string)  { a.APIKey = apiKey }
func (a *Anthropic) SetBaseURL(url string)    { a.BaseURL = url }
func (a *Anthropic) SetMaxTokens(n int)       { a.maxTokens = n }
func (a *Anthropic) MaxTokens() int            { return a.maxTokens }
func (a *Anthropic) SetDisableThinking(disable bool) {
	if disable {
		a.thinkingType = "disabled"
	} else if a.thinkingBudget > 0 {
		a.thinkingType = "enabled"
	} else {
		a.thinkingType = "adaptive"
	}
}
func (a *Anthropic) SetThinkingBudget(tokens int) {
	if tokens < 0 {
		a.thinkingType = "disabled"
	} else if tokens > 0 {
		a.thinkingType = "enabled"
		a.thinkingBudget = tokens
	}
	// tokens == 0: keep default (adaptive)
}
func (a *Anthropic) SetReasoningEffort(effort string) {
	// Translate OpenAI-compat effort levels to Anthropic thinking config.
	switch effort {
	case "max":
		a.thinkingType = "enabled"
		a.thinkingBudget = 16000
	case "high":
		a.thinkingType = "enabled"
		a.thinkingBudget = 8000
	case "low":
		a.thinkingType = "disabled"
	default:
		// "" or "medium" / unknown — use adaptive (let model decide).
		a.thinkingType = "adaptive"
		a.thinkingBudget = 0
	}
}

type cacheControl struct {
	Type string `json:"type"`
}

type anthropicTool struct {
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	InputSchema  interface{}     `json:"input_schema"`
	CacheControl *cacheControl   `json:"cache_control,omitempty"`
}

type anthropicContentBlock struct {
	Type         string          `json:"type"`
	Text         string          `json:"text,omitempty"`
	ID           string          `json:"id,omitempty"`
	Name         string          `json:"name,omitempty"`
	Input        json.RawMessage `json:"input,omitempty"`
	ToolUseID    string          `json:"tool_use_id,omitempty"`
	Content      string          `json:"content,omitempty"`
	CacheControl *cacheControl   `json:"cache_control,omitempty"`
}

type anthropicRequest struct {
	Model       string          `json:"model"`
	MaxTokens   int             `json:"max_tokens"`
	Temperature float64         `json:"temperature"`
	System      interface{}     `json:"system,omitempty"`
	Messages    []anthropicMsg  `json:"messages"`
	Tools       []anthropicTool `json:"tools,omitempty"`
	Stream      bool            `json:"stream"`
	Thinking    interface{}     `json:"thinking,omitempty"`
}

type anthropicMsg struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"`
}

type anthropicResponse struct {
	ID      string                  `json:"id"`
	Content []anthropicContentBlock `json:"content"`
	Usage   struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// --- Anthropic SSE streaming types ---

type anthropicSSEEvent struct {
	Type    string          `json:"type"`
	Index   int             `json:"index"`
	Delta   json.RawMessage `json:"delta"`
	Message struct {
		Usage struct {
			InputTokens              int `json:"input_tokens"`
			CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
			CacheReadInputTokens     int `json:"cache_read_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
	Usage *struct {
		OutputTokens             int `json:"output_tokens"`
		CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
		CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	} `json:"usage"`
	ContentBlock json.RawMessage `json:"content_block"`
}

type anthropicTextDelta struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type anthropicInputJSONDelta struct {
	Type        string `json:"type"`
	PartialJSON string `json:"partial_json"`
}

func toAnthropicTools(tools []ToolDef) []anthropicTool {
	if len(tools) == 0 {
		return nil
	}
	result := make([]anthropicTool, len(tools))
	for i, t := range tools {
		result[i] = anthropicTool{
			Name:        t.Function.Name,
			Description: t.Function.Description,
			InputSchema: t.Function.Parameters,
		}
	}
	return result
}

// addCacheControl attaches a cache_control breakpoint to the last content block
// of an anthropic message. Handles both string content (user/assistant text) and
// array content (tool results, assistant with tool calls).
func addCacheControl(content interface{}) interface{} {
	switch c := content.(type) {
	case string:
		return []anthropicContentBlock{{
			Type:         "text",
			Text:         c,
			CacheControl: &cacheControl{Type: "ephemeral"},
		}}
	case []anthropicContentBlock:
		if len(c) > 0 {
			c[len(c)-1].CacheControl = &cacheControl{Type: "ephemeral"}
		}
		return c
	}
	return content
}

func toAnthropicMessages(messages []Message) ([]anthropicMsg, string) {
	var systemPrompt string
	var result []anthropicMsg

	for _, msg := range messages {
		if msg.Role == "system" {
			systemPrompt += msg.Content
			continue
		}

		role := msg.Role

		if msg.Role == "tool" {
			result = append(result, anthropicMsg{
				Role: "user",
				Content: []anthropicContentBlock{{
					Type:      "tool_result",
					ToolUseID: msg.ToolCallID,
					Content:   msg.Content,
				}},
			})
			continue
		}

		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			blocks := make([]anthropicContentBlock, 0, len(msg.ToolCalls)+1)
			if msg.Content != "" {
				blocks = append(blocks, anthropicContentBlock{Type: "text", Text: msg.Content})
			}
			for _, tc := range msg.ToolCalls {
				blocks = append(blocks, anthropicContentBlock{
					Type:  "tool_use",
					ID:    tc.ID,
					Name:  tc.Function.Name,
					Input: json.RawMessage(tc.Function.Arguments),
				})
			}
			result = append(result, anthropicMsg{Role: role, Content: blocks})
			continue
		}

		result = append(result, anthropicMsg{Role: role, Content: msg.Content})
	}

	return result, systemPrompt
}

func (a *Anthropic) buildRequest(messages []Message, tools []ToolDef, stream bool) (*anthropicRequest, error) {
	anthropicMsgs, systemPrompt := toAnthropicMessages(messages)
	anthTools := toAnthropicTools(tools)

	// --- Prompt caching: mark cache breakpoints for Anthropic's prompt cache ---
	// Cache TTL is 5 minutes. Cache hits reduce input cost by 90%.
	// Strategy:
	//   1. System prompt — always cached (static, max ROI)
	//   2. Tool definitions — cache on last tool (rarely change)
	//   3. Messages — cache all but last 3 (the current turn)

	// 1. System prompt: wrap in array with cache_control.
	var system interface{}
	if systemPrompt != "" {
		system = []anthropicContentBlock{{
			Type:         "text",
			Text:         systemPrompt,
			CacheControl: &cacheControl{Type: "ephemeral"},
		}}
	}

	// 2. Tool definitions: cache on last tool.
	if len(anthTools) > 0 {
		anthTools[len(anthTools)-1].CacheControl = &cacheControl{Type: "ephemeral"}
	}

	// 3. Messages: cache all but last 3 (current turn stays hot).
	if len(anthropicMsgs) > 3 {
		anthropicMsgs[len(anthropicMsgs)-4].Content = addCacheControl(anthropicMsgs[len(anthropicMsgs)-4].Content)
	}

	req := &anthropicRequest{
		Model:       a.Model,
		MaxTokens:   a.maxTokens,
		Temperature: a.temperature,
		System:      system,
		Messages:    anthropicMsgs,
		Tools:       anthTools,
		Stream:      stream,
	}
	switch a.thinkingType {
	case "disabled":
		req.Thinking = map[string]string{"type": "disabled"}
	case "enabled":
		budget := a.thinkingBudget
		if budget == 0 {
			budget = min(16000, a.maxTokens/2)
		}
		if budget == 0 {
			budget = 1 // safeguard: API rejects budget_tokens=0
		}
		if budget >= a.maxTokens {
			budget = a.maxTokens - 1
		}
		req.Thinking = map[string]interface{}{
			"type":          "enabled",
			"budget_tokens": budget,
		}
		req.Temperature = 1 // API requires temperature when thinking is enabled
	default: // "adaptive" — DeepSeek ignores it → no thinking
		req.Thinking = map[string]string{"type": "adaptive"}
	}
	// Debug: write request to /tmp so we can inspect Anthropic API calls.
	return req, nil
}

func (a *Anthropic) Chat(ctx context.Context, messages []Message, tools []ToolDef) (*Response, error) {
	body, err := a.buildRequest(messages, tools, false)
	if err != nil {
		return nil, err
	}

	jsonBody, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, "POST", a.BaseURL+"/messages", bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", a.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("anthropic-beta", "prompt-caching-2024-07-31")

	resp, err := SharedHTTPClientTimeout.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error: %s - %s", resp.Status, string(respBody))
	}

	var anthResp anthropicResponse
	if err := json.Unmarshal(respBody, &anthResp); err != nil {
		return nil, err
	}

	return anthropicToResponse(&anthResp), nil
}

func anthropicToResponse(anth *anthropicResponse) *Response {
	resp := &Response{ID: anth.ID}

	var textContent string
	var toolCalls []ToolCall

	for _, block := range anth.Content {
		switch block.Type {
		case "text":
			textContent += block.Text
		case "tool_use":
			toolCalls = append(toolCalls, ToolCall{
				ID:   block.ID,
				Type: "function",
				Function: FunctionCall{
					Name:      block.Name,
					Arguments: string(block.Input),
				},
			})
		}
	}

	resp.Choices = []Choice{{
		Message: Message{
			Role:      "assistant",
			Content:   textContent,
			ToolCalls: toolCalls,
		},
	}}

	return resp
}

func (a *Anthropic) ChatStream(ctx context.Context, messages []Message, tools []ToolDef) (<-chan StreamToken, <-chan error) {
	tokenChan := make(chan StreamToken)
	errChan := make(chan error, 1)

	go func() {
		defer close(tokenChan)
		defer close(errChan)

		body, err := a.buildRequest(messages, tools, true)
		if err != nil {
			errChan <- err
			return
		}

		jsonBody, _ := json.Marshal(body)
		req, err := http.NewRequestWithContext(ctx, "POST", a.BaseURL+"/messages", bytes.NewBuffer(jsonBody))
		if err != nil {
			errChan <- err
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("x-api-key", a.APIKey)
		req.Header.Set("anthropic-version", "2023-06-01")
		req.Header.Set("anthropic-beta", "prompt-caching-2024-07-31")

		resp, err := SharedHTTPStreamClient.Do(req)
		if err != nil {
			errChan <- err
			return
		}
		defer func() { _ = resp.Body.Close() }()

		// Force-close body on context cancellation so scanner.Scan() unblocks.
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
			errChan <- fmt.Errorf("API error (HTTP %d): %s", resp.StatusCode, string(body))
			return
		}

		scanner := bufio.NewScanner(resp.Body)
		// Track tool use accumulators per content block index.
		type toolAccum struct {
			id   string
			name string
			args strings.Builder
		}
		toolAccums := make(map[int]*toolAccum)

		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")

			var event anthropicSSEEvent
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				continue
			}

			switch event.Type {
			case "message_start":
				if event.Message.Usage.InputTokens > 0 {
					tokenChan <- StreamToken{
						Usage: &StreamUsage{
							PromptTokens:            event.Message.Usage.InputTokens,
							CacheCreationInputTokens: event.Message.Usage.CacheCreationInputTokens,
							CacheReadInputTokens:    event.Message.Usage.CacheReadInputTokens,
						},
					}
				}
			case "content_block_start":
				var cb anthropicContentBlock
				if err := json.Unmarshal(event.ContentBlock, &cb); err != nil {
					continue
				}
				if cb.Type == "tool_use" {
					toolAccums[event.Index] = &toolAccum{
						id:   cb.ID,
						name: cb.Name,
					}
				}

			case "content_block_delta":
				// Try text_delta first.
				var td anthropicTextDelta
				if json.Unmarshal(event.Delta, &td) == nil && td.Type == "text_delta" {
					tokenChan <- StreamToken{Content: td.Text}
					continue
				}
				// Try input_json_delta.
				var ijd anthropicInputJSONDelta
				if json.Unmarshal(event.Delta, &ijd) == nil && ijd.Type == "input_json_delta" {
					if acc := toolAccums[event.Index]; acc != nil {
						acc.args.WriteString(ijd.PartialJSON)
						tokenChan <- StreamToken{
							ToolCallDelta: &ToolCallDelta{
								Index:     event.Index,
								ID:        acc.id,
								Name:      acc.name,
								Arguments: ijd.PartialJSON,
							},
						}
					}
				}

			case "message_delta":
				if event.Usage != nil && event.Usage.OutputTokens > 0 {
					u := &StreamUsage{
						CompletionTokens: event.Usage.OutputTokens,
					}
					if event.Usage.CacheCreationInputTokens > 0 {
						u.CacheCreationInputTokens = event.Usage.CacheCreationInputTokens
					}
					if event.Usage.CacheReadInputTokens > 0 {
						u.CacheReadInputTokens = event.Usage.CacheReadInputTokens
					}
					tokenChan <- StreamToken{Usage: u}
				}
			}
		}
		close(done)
			if err := scanner.Err(); err != nil {
				if ctx.Err() != nil {
					errChan <- ctx.Err()
				} else {
					errChan <- err
				}
			}
	}()

	return tokenChan, errChan
}
