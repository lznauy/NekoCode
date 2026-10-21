package media

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"nekocode/bot/config"
)

const imageUnderstandingTruncated = "\n[Image result truncated at max_tokens; increase max_tokens (up to 4096) or narrow the question.]"

func (t *ImageUnderstandTool) executeOpenAIUnderstanding(ctx context.Context, cfg config.ImageUnderstandConfig, prompt string, images []understandingImage, maxTokens int, onText func()) (string, error) {
	endpoint, err := imageUnderstandEndpoint(cfg.BaseURL, "openai")
	if err != nil {
		return "", fmt.Errorf("invalid image understand base URL: %w", err)
	}
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
		"max_tokens": maxTokens,
		"stream":     true,
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
	resp, err := t.post(ctx, endpoint, map[string]string{
		"Authorization": "Bearer " + cfg.APIKey,
		"Accept":        "text/event-stream",
	}, body)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream") {
		return readImageUnderstandingStream(resp.Body, onText)
	}
	// Some compatible gateways return JSON even when streaming is requested.
	var response struct {
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
			Error   json.RawMessage `json:"error"`
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
		if len(chunk.Error) > 0 && string(chunk.Error) != "null" {
			return "", fmt.Errorf("image model stream error: %s", chunk.Error)
		}
		if len(chunk.Choices) > 0 {
			if chunk.Choices[0].Delta.Content != "" && onText != nil {
				onText()
			}
			output.WriteString(chunk.Choices[0].Delta.Content)
			if chunk.Choices[0].FinishReason != "" {
				complete = true
				truncated = chunk.Choices[0].FinishReason == "length"
				// The result is complete; do not wait for a gateway to close
				// its connection or send an optional trailing usage event.
				break
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

func (t *ImageUnderstandTool) executeAnthropicUnderstanding(ctx context.Context, cfg config.ImageUnderstandConfig, prompt string, images []understandingImage, maxTokens int) (string, error) {
	endpoint, err := imageUnderstandEndpoint(cfg.BaseURL, "anthropic")
	if err != nil {
		return "", fmt.Errorf("invalid image understand base URL: %w", err)
	}
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
		StopReason string `json:"stop_reason"`
		Content    []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := t.postJSON(ctx, endpoint, map[string]string{
		"x-api-key":         cfg.APIKey,
		"anthropic-version": "2023-06-01",
	}, body, &response); err != nil {
		return "", err
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
