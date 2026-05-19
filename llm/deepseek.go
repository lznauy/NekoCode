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
	"sync/atomic"
)

// DeepSeek is a self-contained client for the DeepSeek API.
type DeepSeek struct {
	APIKey          string
	BaseURL         string
	Model           string
	maxTokens       int
	temperature     float64
	disableThinking bool
	reasoningEffort string

	cacheHitTokens  atomic.Int64
	cacheMissTokens atomic.Int64
}

func NewDeepSeek(apiKey, baseURL, model string) *DeepSeek {
	if baseURL == "" {
		baseURL = "https://api.deepseek.com/v1"
	}
	return &DeepSeek{
		APIKey:      apiKey,
		BaseURL:     baseURL,
		Model:       model,
		maxTokens:   32000,
		temperature: 0.3,
	}
}

func (c *DeepSeek) SetAPIKey(apiKey string)    { c.APIKey = apiKey }
func (c *DeepSeek) SetBaseURL(url string)      { c.BaseURL = url }
func (c *DeepSeek) SetMaxTokens(n int)         { c.maxTokens = n }
func (c *DeepSeek) MaxTokens() int             { return c.maxTokens }
func (c *DeepSeek) SetDisableThinking(d bool)  { c.disableThinking = d }
func (c *DeepSeek) SetReasoningEffort(e string) { c.reasoningEffort = e }

func (c *DeepSeek) SetThinkingBudget(tokens int) {
	if tokens < 0 {
		c.disableThinking = true
		return
	}
	c.disableThinking = false
	switch {
	case tokens <= 4000:
		c.reasoningEffort = "low"
	case tokens <= 8000:
		c.reasoningEffort = "high"
	default:
		c.reasoningEffort = "max"
	}
}

func (c *DeepSeek) CacheStats() (hit, miss int64) {
	return c.cacheHitTokens.Load(), c.cacheMissTokens.Load()
}

func (c *DeepSeek) CacheHitRatio() float64 {
	hit := c.cacheHitTokens.Load()
	miss := c.cacheMissTokens.Load()
	if total := hit + miss; total > 0 {
		return float64(hit) / float64(total)
	}
	return 0
}

func (c *DeepSeek) Chat(ctx context.Context, messages []Message, tools []ToolDef) (*Response, error) {
	body := c.buildBody(messages, tools, false)
	jsonBody, _ := json.Marshal(body)

	req, _ := http.NewRequestWithContext(ctx, "POST", c.BaseURL+"/chat/completions", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)

	resp, err := SharedHTTPClientTimeout.Do(req)
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

	var r Response
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

func (c *DeepSeek) ChatStream(ctx context.Context, messages []Message, tools []ToolDef) (<-chan StreamToken, <-chan error) {
	body := c.buildBody(messages, tools, true)
	jsonBody, _ := json.Marshal(body)

	req, _ := http.NewRequestWithContext(ctx, "POST", c.BaseURL+"/chat/completions", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)

	resp, err := SharedHTTPStreamClient.Do(req)
	if err != nil {
		ch := make(chan StreamToken)
		ech := make(chan error, 1)
		ech <- err
		close(ch)
		return ch, ech
	}

	tokenCh := make(chan StreamToken)
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

		var lastUsage *StreamUsage
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

			var chunk StreamChunk
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				continue
			}
			if len(chunk.Choices) == 0 {
				continue
			}
			delta := chunk.Choices[0].Delta
			token := StreamToken{
				Content:          delta.Content,
				ReasoningContent: delta.ReasoningContent,
				Usage:            chunk.Usage,
				FinishReason:     chunk.Choices[0].FinishReason,
			}
			if token.Content != "" || token.ReasoningContent != "" || token.Usage != nil || token.FinishReason != "" {
				tokenCh <- token
				if token.Usage != nil {
					lastUsage = token.Usage
				}
			}
			for _, tc := range delta.ToolCalls {
				tokenCh <- StreamToken{
					ToolCallDelta: &ToolCallDelta{
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

		if lastUsage != nil {
			if lastUsage.CacheHitTokens > 0 {
				c.cacheHitTokens.Add(int64(lastUsage.CacheHitTokens))
			}
			if lastUsage.CacheMissTokens > 0 {
				c.cacheMissTokens.Add(int64(lastUsage.CacheMissTokens))
			}
		}
	}()

	return tokenCh, errCh
}

func (c *DeepSeek) buildBody(messages []Message, tools []ToolDef, stream bool) map[string]any {
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
