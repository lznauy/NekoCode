package builtin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"nekocode/bot/config"
	"nekocode/bot/sdk"
	"nekocode/bot/tools"
	"nekocode/common"
)

type ImageGenTool struct {
	client *http.Client
	models []config.ImageGenConfig
}

func NewImageGenTool(models []config.ImageGenConfig) *ImageGenTool {
	return &ImageGenTool{
		client: tools.NewToolHTTPClient(120 * time.Second),
		models: models,
	}
}

func (t *ImageGenTool) Name() string { return "image_gen" }

func (t *ImageGenTool) ExecutionMode(map[string]any) tools.ExecutionMode {
	return tools.ModeSequential
}
func (t *ImageGenTool) DangerLevel(map[string]any) common.DangerLevel {
	return common.LevelSafe
}

func (t *ImageGenTool) Description() string {
	return "Generate images from text prompts using configured text-to-image models. Images are saved to local files and links are returned."
}

func (t *ImageGenTool) Parameters() []tools.Parameter {
	return []tools.Parameter{
		{Name: "prompt", Type: "string", Required: true, Description: "Chinese or English text prompt for image generation, recommended ≤120 chars, max 800"},
		{Name: "output_dir", Type: "string", Required: false, Description: "Directory to save generated images. Defaults to current working directory."},
		{Name: "width", Type: "integer", Required: false, Description: "Image width, default 1328. Must also set height."},
		{Name: "height", Type: "integer", Required: false, Description: "Image height, default 1328. Must also set width."},
		{Name: "seed", Type: "integer", Required: false, Description: "Random seed, -1 for random (default)"},
		{Name: "model", Type: "string", Required: false, Description: "Image gen model name from config. Uses first configured model if omitted."},
	}
}

func (t *ImageGenTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	prompt, ok := args["prompt"].(string)
	if !ok || strings.TrimSpace(prompt) == "" {
		return "", fmt.Errorf("missing prompt parameter")
	}

	cfg := t.resolveModel(args)
	if cfg.Name == "" {
		return "", fmt.Errorf("no image gen models configured — add image_gen_models in ~/.nekocode/config.json")
	}

	outputDir, _ := args["output_dir"].(string)
	if outputDir == "" {
		outputDir, _ = os.Getwd()
	}
	if _, err := tools.ValidatePath(outputDir); err != nil {
		return "", fmt.Errorf("invalid output_dir: %w", err)
	}

	switch cfg.Provider {
	case "jimeng":
		return t.executeJimeng(ctx, cfg, prompt, outputDir, args)
	default:
		return "", fmt.Errorf("unsupported image gen provider: %s", cfg.Provider)
	}
}

func (t *ImageGenTool) resolveModel(args map[string]any) config.ImageGenConfig {
	if name, _ := args["model"].(string); name != "" {
		for _, m := range t.models {
			if m.Name == name {
				return m
			}
		}
	}
	if len(t.models) > 0 {
		return t.models[0]
	}
	return config.ImageGenConfig{}
}

// -- Jimeng provider --------------------------------------------------------

type jimengSubmitResp struct {
	Code    int                    `json:"code"`
	Data    jimengSubmitRespData   `json:"data"`
	Message string                 `json:"message"`
}

type jimengSubmitRespData struct {
	TaskID string `json:"task_id"`
}

type jimengQueryResp struct {
	Code    int                   `json:"code"`
	Data    jimengQueryRespData   `json:"data"`
	Message string                `json:"message"`
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

	signer := sdk.NewVolcSigner(cfg.APIKey, cfg.SecretKey, "cn-north-1", "cv")

	width := intArg(args, "width", 1328)
	height := intArg(args, "height", 1328)
	seed := intArg(args, "seed", -1)

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

	queryBody := map[string]any{
		"req_key": model,
		"task_id": taskID,
		"req_json": `{"return_url":true}`,
	}

	result, err := t.jimengPoll(ctx, signer, baseURL, queryBody)
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

func (t *ImageGenTool) downloadImages(ctx context.Context, urls []string, dir string) (string, error) {
	var sb strings.Builder
	sb.WriteString("Generated images:\n")
	for i, imgURL := range urls {
		ext := ".jpg"
		if idx := strings.LastIndex(imgURL, "."); idx != -1 {
			if e := strings.ToLower(imgURL[idx:]); strings.HasPrefix(e, ".png") || strings.HasPrefix(e, ".jpg") || strings.HasPrefix(e, ".jpeg") || strings.HasPrefix(e, ".webp") {
				if end := strings.Index(imgURL[idx:], "?"); end != -1 {
					ext = imgURL[idx : idx+end]
				} else {
					ext = imgURL[idx:]
				}
			}
		}
		filename := fmt.Sprintf("nekocode_img_%s_%d%s", time.Now().Format("20060102_150405"), i+1, ext)
		savePath := filepath.Join(dir, filename)

		if err := t.downloadFile(ctx, imgURL, savePath); err != nil {
			return "", fmt.Errorf("download image %d: %w", i+1, err)
		}
		fmt.Fprintf(&sb, "  %s\n  => %s\n", imgURL, savePath)
	}
	return sb.String(), nil
}

func (t *ImageGenTool) saveBase64Images(_ []string, _ string) (string, error) {
	return "", fmt.Errorf("base64 image saving not yet implemented")
}

func (t *ImageGenTool) downloadFile(ctx context.Context, url, path string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := t.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, io.LimitReader(resp.Body, 20<<20))
	return err
}

func (t *ImageGenTool) jimengSubmit(ctx context.Context, signer *sdk.VolcSigner, baseURL string, body map[string]any) (string, error) {
	return t.jimengCall(ctx, signer, baseURL, "CVSync2AsyncSubmitTask", body)
}

func (t *ImageGenTool) jimengPoll(ctx context.Context, signer *sdk.VolcSigner, baseURL string, queryBody map[string]any) (*jimengQueryRespData, error) {
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

func (t *ImageGenTool) jimengCall(ctx context.Context, signer *sdk.VolcSigner, baseURL, action string, body map[string]any) (string, error) {
	respBody, err := t.jimengCallRaw(ctx, signer, baseURL, action, body)
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

func (t *ImageGenTool) jimengCallRaw(ctx context.Context, signer *sdk.VolcSigner, baseURL, action string, body map[string]any) ([]byte, error) {
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal body: %w", err)
	}

	query := url.Values{}
	query.Set("Action", action)
	query.Set("Version", "2022-08-31")

	reqURL := baseURL + "?" + query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	host := req.URL.Host
	sr, err := signer.Sign("POST", "/", host, query, bodyBytes)
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

func intArg(args map[string]any, key string, defaultVal int) int {
	v, ok := args[key]
	if !ok {
		return defaultVal
	}
	// JSON numbers come as float64 from unmarshaled args
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	}
	return defaultVal
}
