package media

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"nekocode/bot/config"
)

func (t *ImageUnderstandTool) executeOpenAIUnderstanding(ctx context.Context, cfg config.ImageUnderstandConfig, prompt, mediaType string, imageData []byte, maxTokens int) (string, error) {
	endpoint, err := imageUnderstandEndpoint(cfg.BaseURL, "openai")
	if err != nil {
		return "", fmt.Errorf("invalid image understand base URL: %w", err)
	}
	body := map[string]any{
		"model": cfg.Model,
		"messages": []any{map[string]any{
			"role": "user",
			"content": []any{
				map[string]any{"type": "text", "text": prompt},
				map[string]any{"type": "image_url", "image_url": map[string]any{
					"url": "data:" + mediaType + ";base64," + encodedImage(imageData),
				}},
			},
		}},
		"max_tokens": maxTokens,
	}
	var response struct {
		Choices []struct {
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := t.postJSON(ctx, endpoint, map[string]string{
		"Authorization": "Bearer " + cfg.APIKey,
	}, body, &response); err != nil {
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

func (t *ImageUnderstandTool) executeAnthropicUnderstanding(ctx context.Context, cfg config.ImageUnderstandConfig, prompt, mediaType string, imageData []byte, maxTokens int) (string, error) {
	endpoint, err := imageUnderstandEndpoint(cfg.BaseURL, "anthropic")
	if err != nil {
		return "", fmt.Errorf("invalid image understand base URL: %w", err)
	}
	body := map[string]any{
		"model":      cfg.Model,
		"max_tokens": maxTokens,
		"messages": []any{map[string]any{
			"role": "user",
			"content": []any{
				map[string]any{"type": "image", "source": map[string]any{
					"type": "base64", "media_type": mediaType, "data": encodedImage(imageData),
				}},
				map[string]any{"type": "text", "text": prompt},
			},
		}},
	}
	var response struct {
		Content []struct {
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
	return strings.Join(output, "\n"), nil
}
