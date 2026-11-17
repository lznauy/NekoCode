package media

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	neturl "net/url"
	"strings"
	"time"

	"nekocode/bot/config"
	"nekocode/bot/extension/tool/runtime/toolutil"
	"nekocode/util/url"
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
	seed, err := toolutil.OptIntArg(args, "seed", -1)
	if err != nil {
		return "", err
	}
	width, err := toolutil.OptIntArg(args, "width", 1328)
	if err != nil {
		return "", err
	}
	height, err := toolutil.OptIntArg(args, "height", 1328)
	if err != nil {
		return "", err
	}

	signer := NewSigner(cfg.APIKey, cfg.SecretKey, "cn-north-1", "cv")
	submitBody := map[string]any{
		"req_key": model,
		"prompt":  prompt,
		"seed":    seed,
	}
	if width > 0 && height > 0 {
		submitBody["width"] = width
		submitBody["height"] = height
	}

	taskID, err := t.jimengSubmit(ctx, signer, baseURL, submitBody)
	if err != nil {
		return "", fmt.Errorf("submit: %w", err)
	}

	if strings.TrimSpace(taskID) == "" {
		return "", fmt.Errorf("generation submission returned no task ID; outcome unknown, do not resubmit automatically")
	}
	_, explicit := args["output_dir"]
	task := imageTask{TaskID: taskID, Config: cfg, OutputDir: outputDir, ExplicitDir: explicit, Scope: imageScope(ctx)}
	token := t.rememberImage(task)
	return t.collectImage(ctx, token, task)
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
	query := neturl.Values{}
	query.Set("Action", action)
	query.Set("Version", "2022-08-31")

	endpoint, err := jimengEndpoint(baseURL, query)
	if err != nil {
		return nil, fmt.Errorf("build endpoint: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	signPath := req.URL.EscapedPath()
	if signPath == "" {
		signPath = "/"
	}
	sr, err := signer.Sign("POST", signPath, req.URL.Host, query, bodyBytes)
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
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}
	return respBody, nil
}

func jimengEndpoint(baseURL string, query neturl.Values) (string, error) {
	u, err := neturl.Parse(url.JoinURLPath(baseURL))
	if err != nil {
		return "", err
	}
	if u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("invalid base_url %q", baseURL)
	}
	u.RawQuery = query.Encode()
	return u.String(), nil
}
