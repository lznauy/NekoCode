package media

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"nekocode/bot/config"
	providertypes "nekocode/bot/provider/types"
)

const imageUnderstandingTruncated = "\n[Image result truncated at max_tokens; increase max_tokens (up to 4096) or narrow the question.]"

func (t *ImageUnderstandTool) executeOpenAIUnderstanding(ctx context.Context, cfg config.ImageUnderstandConfig, endpoint, prompt string, images []understandingImage, maxTokens int, onText func()) (string, error) {
	content := []any{map[string]any{"type": "text", "text": prompt}}
	for i, img := range images {
		if len(images) > 1 {
			content = append(content, map[string]any{"type": "text", "text": fmt.Sprintf("Image %d:", i+1)})
		}
		content = append(content, map[string]any{"type": "image_url", "image_url": map[string]any{
			"url": "data:" + img.mediaType + ";base64," + encodedImage(img.data),
		}})
	}
	body := map[string]any{
		"model": cfg.Model,
		"messages": []any{map[string]any{
			"role":    "user",
			"content": content,
		}},
		"max_tokens":     maxTokens,
		"stream":         true,
		"stream_options": map[string]bool{"include_usage": true},
	}
	// Use the same model capability contract as the main provider. Avoid
	// sending provider-specific thinking fields to models that lack them.
	capability := config.ReasoningCapabilityFor(config.ModelConfig{Model: cfg.Model, Protocol: "openai"})
	if capability.ThinkingMode != "" {
		body["thinking"] = map[string]string{"type": "disabled"}
	}
	if capability.DisableEffort != "" {
		body["reasoning_effort"] = capability.DisableEffort
	}
	headers := map[string]string{"Accept": "text/event-stream"}
	if cfg.APIKey != "" {
		headers["Authorization"] = "Bearer " + cfg.APIKey
	}
	resp, err := t.post(ctx, endpoint, headers, body)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream") {
		return readImageUnderstandingStreamUsage(resp.Body, onText, func(u *providertypes.StreamUsage) { observeImageUsage(ctx, u) })
	}
	// Some compatible gateways return JSON even when streaming is requested.
	var response struct {
		Usage   *providertypes.StreamUsage `json:"usage"`
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&response); err != nil {
		return "", err
	}
	observeImageUsage(ctx, response.Usage)
	if len(response.Choices) == 0 {
		return "", fmt.Errorf("image model returned no choices")
	}
	text, err := openAIContentText(response.Choices[0].Message.Content)
	if err != nil {
		return "", err
	}
	if text == "" {
		return "", fmt.Errorf("image model returned an empty response")
	}
	onText()
	if response.Choices[0].FinishReason == "length" {
		text += imageUnderstandingTruncated
	}
	return text, nil
}

func readImageUnderstandingStream(reader io.Reader, onText func()) (string, error) {
	return readImageUnderstandingStreamUsage(reader, onText, nil)
}

func readImageUnderstandingStreamUsage(reader io.Reader, onText func(), onUsage func(*providertypes.StreamUsage)) (string, error) {
	const maxBytes = 4 << 20
	scanner := bufio.NewScanner(io.LimitReader(reader, maxBytes+1))
	scanner.Buffer(make([]byte, 4096), maxBytes+1)
	var output strings.Builder
	complete := false
	truncated := false
	consumed := 0
	for scanner.Scan() {
		consumed += len(scanner.Bytes()) + 1
		if consumed > maxBytes {
			return "", fmt.Errorf("image model stream exceeds response limit")
		}
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			complete = true
			break
		}
		if data == "" {
			continue
		}
		var chunk struct {
			Usage   *providertypes.StreamUsage `json:"usage"`
			Error   json.RawMessage            `json:"error"`
			Choices []struct {
				FinishReason string `json:"finish_reason"`
				Delta        struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return "", fmt.Errorf("decode image model stream: %w", err)
		}
		if chunk.Usage != nil && onUsage != nil {
			onUsage(chunk.Usage)
		}
		if len(chunk.Error) > 0 && string(chunk.Error) != "null" {
			return "", fmt.Errorf("image model stream error: %s", chunk.Error)
		}
		if !complete && len(chunk.Choices) > 0 {
			if chunk.Choices[0].Delta.Content != "" && onText != nil {
				onText()
			}
			output.WriteString(chunk.Choices[0].Delta.Content)
			if chunk.Choices[0].FinishReason != "" {
				complete = true
				truncated = chunk.Choices[0].FinishReason == "length"
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	if !complete {
		return "", fmt.Errorf("image model stream ended before completion")
	}
	text := strings.TrimSpace(output.String())
	if text == "" {
		return "", fmt.Errorf("image model returned an empty response")
	}
	if truncated {
		text += imageUnderstandingTruncated
	}
	return text, nil
}

func openAIContentText(content json.RawMessage) (string, error) {
	var text string
	if err := json.Unmarshal(content, &text); err == nil {
		return strings.TrimSpace(text), nil
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(content, &parts); err != nil {
		return "", fmt.Errorf("decode image model content: %w", err)
	}
	var output []string
	for _, part := range parts {
		if part.Type == "text" && strings.TrimSpace(part.Text) != "" {
			output = append(output, strings.TrimSpace(part.Text))
		}
	}
	return strings.Join(output, "\n"), nil
}

func (t *ImageUnderstandTool) executeAnthropicUnderstanding(ctx context.Context, cfg config.ImageUnderstandConfig, endpoint, prompt string, images []understandingImage, maxTokens int) (string, error) {
	content := []any{map[string]any{"type": "text", "text": prompt}}
	for i, img := range images {
		if len(images) > 1 {
			content = append(content, map[string]any{"type": "text", "text": fmt.Sprintf("Image %d:", i+1)})
		}
		content = append(content, map[string]any{"type": "image", "source": map[string]any{
			"type": "base64", "media_type": img.mediaType, "data": encodedImage(img.data),
		}})
	}
	body := map[string]any{
		"model":      cfg.Model,
		"max_tokens": maxTokens,
		"messages": []any{map[string]any{
			"role":    "user",
			"content": content,
		}},
	}
	var response struct {
		Usage *struct {
			Input       int  `json:"input_tokens"`
			Output      int  `json:"output_tokens"`
			CacheRead   *int `json:"cache_read_input_tokens"`
			CacheCreate *int `json:"cache_creation_input_tokens"`
		} `json:"usage"`
		StopReason string `json:"stop_reason"`
		Content    []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	headers := map[string]string{"anthropic-version": "2023-06-01"}
	if cfg.APIKey != "" {
		headers["x-api-key"] = cfg.APIKey
	}
	if err := t.postJSON(ctx, endpoint, headers, body, &response); err != nil {
		return "", err
	}
	if response.Usage != nil {
		raw := response.Usage
		u := &providertypes.StreamUsage{PromptTokens: raw.Input, CompletionTokens: raw.Output}
		if raw.CacheRead != nil {
			u.CacheUsageReported = true
			u.CacheHitTokens = *raw.CacheRead
			u.PromptTokens += *raw.CacheRead
		}
		if raw.CacheCreate != nil {
			u.PromptTokens += *raw.CacheCreate
		}
		if u.CacheUsageReported {
			u.CacheMissTokens = u.PromptTokens - u.CacheHitTokens
		}
		observeImageUsage(ctx, u)
	}
	var output []string
	for _, block := range response.Content {
		if block.Type == "text" && strings.TrimSpace(block.Text) != "" {
			output = append(output, strings.TrimSpace(block.Text))
		}
	}
	if len(output) == 0 {
		return "", fmt.Errorf("image model returned an empty response")
	}
	text := strings.Join(output, "\n")
	if response.StopReason == "max_tokens" {
		text += imageUnderstandingTruncated
	}
	return text, nil
}
