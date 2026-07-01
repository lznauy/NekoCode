package media

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"time"

	"nekocode/bot/config"
	"nekocode/bot/tools/toolhelpers"
)

type jimengSubmitResp struct {
	Code    int                  `json:"code"`
	Data    jimengSubmitRespData `json:"data"`
	Message string               `json:"message"`
}

type jimengSubmitRespData struct {
	TaskID string `json:"task_id"`
}

type jimengQueryResp struct {
	Code    int                 `json:"code"`
	Data    jimengQueryRespData `json:"data"`
	Message string              `json:"message"`
}

type jimengQueryRespData struct {
	BinaryDataBase64 []string `json:"binary_data_base64"`
	ImageURLs        []string `json:"image_urls"`
	Status           string   `json:"status"`
}

func (t *ImageGenTool) executeJimeng(ctx context.Context, cfg config.ImageGenConfig, prompt, outputDir string, args map[string]any) (string, error) {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://visual.volcengineapi.com"
	}
	model := cfg.Model
	if model == "" {
		model = "jimeng_t2i_v31"
	}

	signer := NewSigner(cfg.APIKey, cfg.SecretKey, "cn-north-1", "cv")
	submitBody := map[string]any{
		"req_key": model,
		"prompt":  prompt,
		"seed":    toolhelpers.OptIntArg(args, "seed", -1),
	}
	width := toolhelpers.OptIntArg(args, "width", 1328)
	height := toolhelpers.OptIntArg(args, "height", 1328)
	if width > 0 && height > 0 {
		submitBody["width"] = width
		submitBody["height"] = height
	}

	taskID, err := t.jimengSubmit(ctx, signer, baseURL, submitBody)
	if err != nil {
		return "", fmt.Errorf("submit: %w", err)
	}

	result, err := t.jimengPoll(ctx, signer, baseURL, map[string]any{
		"req_key":  model,
		"task_id":  taskID,
		"req_json": `{"return_url":true}`,
	})
	if err != nil {
		return "", fmt.Errorf("poll: %w", err)
	}

	if len(result.ImageURLs) > 0 {
		return t.downloadImages(ctx, result.ImageURLs, outputDir)
	}
	if len(result.BinaryDataBase64) > 0 {
		return t.saveBase64Images(result.BinaryDataBase64, outputDir)
	}
	return "", fmt.Errorf("no images returned, status: %s", result.Status)
}

func (t *ImageGenTool) jimengSubmit(ctx context.Context, signer *Signer, baseURL string, body map[string]any) (string, error) {
	respBody, err := t.jimengCallRaw(ctx, signer, baseURL, "CVSync2AsyncSubmitTask", body)
	if err != nil {
		return "", err
	}
	var result jimengSubmitResp
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}
	if result.Code != 10000 {
		return "", fmt.Errorf("API error %d: %s", result.Code, result.Message)
	}
	return result.Data.TaskID, nil
}

func (t *ImageGenTool) jimengPoll(ctx context.Context, signer *Signer, baseURL string, queryBody map[string]any) (*jimengQueryRespData, error) {
	deadline := time.Now().Add(60 * time.Second)
	backoff := 500 * time.Millisecond

	for {
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timed out waiting for image generation")
		}
		respBody, err := t.jimengCallRaw(ctx, signer, baseURL, "CVSync2AsyncGetResult", queryBody)
		if err != nil {
			return nil, err
		}
		var result jimengQueryResp
		if err := json.Unmarshal(respBody, &result); err != nil {
			return nil, fmt.Errorf("parse response: %w", err)
		}
		if result.Code != 10000 {
			return nil, fmt.Errorf("API error %d: %s", result.Code, result.Message)
		}
		switch result.Data.Status {
		case "done":
			return &result.Data, nil
		case "in_queue", "generating":
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
			backoff = time.Duration(math.Min(float64(backoff*2), float64(5*time.Second)))
		default:
			return nil, fmt.Errorf("task %s: %s", result.Data.Status, result.Message)
		}
	}
}

func (t *ImageGenTool) jimengCallRaw(ctx context.Context, signer *Signer, baseURL, action string, body map[string]any) ([]byte, error) {
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal body: %w", err)
	}
	query := url.Values{}
	query.Set("Action", action)
	query.Set("Version", "2022-08-31")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"?"+query.Encode(), bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	sr, err := signer.Sign("POST", "/", req.URL.Host, query, bodyBytes)
	if err != nil {
		return nil, fmt.Errorf("sign: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Date", sr.XDate)
	req.Header.Set("X-Content-Sha256", sr.XContentSha256)
	req.Header.Set("Authorization", sr.Authorization)

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}
	return respBody, nil
}
