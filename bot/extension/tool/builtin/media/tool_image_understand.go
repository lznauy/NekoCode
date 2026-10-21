package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"nekocode/bot/config"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/toolutil"
	"nekocode/bot/extension/tool/runtime/workspace"
	"nekocode/logger"
	utilhttp "nekocode/util/http"
	utilurl "nekocode/util/url"
)

const (
	maxImageUnderstandBytes = 20 << 20
	defaultFastImagePrompt  = "Briefly describe only task-relevant visible facts, text and errors. Do not include reasoning. For multiple images, identify results by image number."
	defaultAccuratePrompt   = "Read visible text and task-relevant details precisely. Answer concisely without reasoning. Mark uncertainty. For multiple images, identify results by image number."
	approvedImageDigestArg  = "_nekocode_approved_image_sha256"
	unavailableImageDigest  = "unavailable"
	fastImageMaxTokens      = 1024
)

type imageUnderstandMode string

const (
	imageUnderstandModeFast     imageUnderstandMode = "fast"
	imageUnderstandModeAccurate imageUnderstandMode = "accurate"
)

type ImageUnderstandTool struct {
	toolutil.SequentialSafeTool
	client *http.Client
	models []config.ImageUnderstandConfig
}

func NewImageUnderstandTool(models []config.ImageUnderstandConfig) *ImageUnderstandTool {
	return &ImageUnderstandTool{
		client: newImageUnderstandHTTPClient(),
		models: append([]config.ImageUnderstandConfig(nil), models...),
	}
}

func (t *ImageUnderstandTool) Name() string { return "image_understand" }

func (t *ImageUnderstandTool) Description() string {
	return "Quickly read 1–4 local images without deep reasoning. Batch related screenshots with paths in one call; results follow input order. Use fast for ordinary screenshots and accurate to preserve small text and original resolution."
}

func (t *ImageUnderstandTool) Parameters() []core.Parameter {
	return []core.Parameter{
		{Name: "path", Type: "string", Description: "Single local PNG, JPEG, WebP, or GIF image. Provide either path or paths."},
		{Name: "paths", Type: "array", Items: &core.Schema{Type: "string"}, Description: "1–4 related local images in order. Combined source size must not exceed 20 MiB. Provide either path or paths."},
		{Name: "prompt", Type: "string", Required: false, Description: "Focused question or analysis instruction. A concise mode-specific prompt is used when omitted."},
		{Name: "mode", Type: "string", Required: false, Enum: []string{string(imageUnderstandModeFast), string(imageUnderstandModeAccurate)}, Description: "fast (default) resizes large images and limits output; accurate preserves the original image and uses the configured accurate model."},
		{Name: "model", Type: "string", Required: false, Description: "Image understanding model name from config. Uses the first configured model if omitted."},
		{Name: "max_tokens", Type: "integer", Description: "Output limit, 1–4096 tokens. Defaults to 1024; increase only for full OCR or detailed extraction."},
	}
}

// PermissionPlan exposes the local file and remote destination in the
// confirmation shown before this data leaves the workspace.
func (t *ImageUnderstandTool) PermissionPlan(args map[string]any, workspaceRoot string) *core.PermissionRequest {
	args[approvedImageDigestArg] = unavailableImageDigest
	paths, pathErr := toolutil.ImageUnderstandingPaths(args)
	var digests []string
	totalBytes := 0
	for i, path := range paths {
		if !filepath.IsAbs(path) && workspaceRoot != "" {
			path = filepath.Join(workspaceRoot, path)
		}
		path = filepath.Clean(path)
		paths[i] = path
		if data, _, err := readImageFile(path); err == nil {
			totalBytes += len(data)
			digests = append(digests, imageDigest(data))
		}
	}
	if pathErr == nil && len(digests) == len(paths) && totalBytes <= maxImageUnderstandBytes {
		args[approvedImageDigestArg] = imageDigest([]byte(strings.Join(digests, ":")))
	}
	mode, err := parseImageUnderstandMode(args)
	if err != nil {
		mode = imageUnderstandModeFast
	}
	cfg, resolveErr := t.resolveUnderstandModel(args, mode)
	destination := "invalid configured endpoint"
	if resolveErr == nil {
		protocol := strings.ToLower(strings.TrimSpace(cfg.Protocol))
		if protocol == "" && strings.EqualFold(cfg.Provider, "anthropic") {
			protocol = "anthropic"
		}
		if protocol == "" {
			protocol = "openai"
		}
		if resolved, endpointErr := imageUnderstandEndpoint(cfg.BaseURL, protocol); endpointErr == nil {
			destination = resolved
		}
	}
	if parsed, err := url.Parse(destination); err == nil && parsed.Host != "" {
		destination = parsed.Host
	}
	return &core.PermissionRequest{
		Reason:       fmt.Sprintf("send local images %s to %s", strings.Join(paths, ", "), destination),
		Capabilities: []string{core.CapNetOutbound},
		Scope:        "once",
		Details:      map[string]any{"workspace": workspaceRoot},
	}
}

func (t *ImageUnderstandTool) Execute(ctx context.Context, args map[string]any) (core.ToolOutput, error) {
	started := time.Now()
	paths, err := toolutil.ImageUnderstandingPaths(args)
	if err != nil {
		return core.ToolOutput{}, err
	}
	maxTokens, err := imageUnderstandOutputTokens(args)
	if err != nil {
		return core.ToolOutput{}, err
	}
	mode, err := parseImageUnderstandMode(args)
	if err != nil {
		return core.ToolOutput{}, err
	}
	cfg, err := t.resolveUnderstandModel(args, mode)
	if err != nil {
		return core.ToolOutput{}, err
	}
	if approved, planned := args[approvedImageDigestArg].(string); planned && approved == unavailableImageDigest {
		return core.ToolOutput{}, fmt.Errorf("image was unavailable during approval; choose a regular local file and approve it again")
	}
	images := make([]understandingImage, 0, len(paths))
	var digests []string
	sourceBytes, sentBytes := 0, 0
	for _, path := range paths {
		imageData, mediaType, err := readImageForUnderstanding(ctx, path)
		if err != nil {
			return core.ToolOutput{}, err
		}
		sourceBytes += len(imageData)
		if sourceBytes > maxImageUnderstandBytes {
			return core.ToolOutput{}, fmt.Errorf("combined images exceed 20 MiB limit")
		}
		digests = append(digests, imageDigest(imageData))
		imageData, mediaType, err = prepareImageForUnderstanding(imageData, mediaType, mode)
		if err != nil {
			return core.ToolOutput{}, err
		}
		sentBytes += len(imageData)
		images = append(images, understandingImage{data: imageData, mediaType: mediaType})
	}
	if approved, planned := args[approvedImageDigestArg].(string); planned && approved != imageDigest([]byte(strings.Join(digests, ":"))) {
		return core.ToolOutput{}, fmt.Errorf("image changed or became unavailable after approval; review and approve it again")
	}
	prepared := time.Since(started)
	var firstText time.Duration
	onText := func() {
		if firstText == 0 {
			firstText = time.Since(started)
		}
	}
	defer func() {
		logger.Log("image_understand: model=%s mode=%s images=%d source_bytes=%d sent_bytes=%d prepare=%s first_text=%s total=%s", cfg.Model, mode, len(images), sourceBytes, sentBytes, prepared, firstText, time.Since(started))
	}()
	prompt := toolutil.OptStringArg(args, "prompt", defaultImagePrompt(mode))

	protocol := strings.ToLower(strings.TrimSpace(cfg.Protocol))
	if protocol == "" {
		protocol = strings.ToLower(strings.TrimSpace(cfg.Provider))
		if protocol != "anthropic" {
			protocol = "openai"
		}
	}

	switch protocol {
	case "openai":
		toolText, toolErr := t.executeOpenAIUnderstanding(ctx, cfg, prompt, images, maxTokens, onText)
		return core.ToolOutput{Text: toolText}, toolErr
	case "anthropic":
		toolText, toolErr := t.executeAnthropicUnderstanding(ctx, cfg, prompt, images, maxTokens)
		if toolErr == nil {
			onText()
		}
		return core.ToolOutput{Text: toolText}, toolErr
	default:
		return core.ToolOutput{}, fmt.Errorf("unsupported image understand protocol: %s", protocol)
	}
}

func parseImageUnderstandMode(args map[string]any) (imageUnderstandMode, error) {
	mode := imageUnderstandMode(strings.ToLower(strings.TrimSpace(toolutil.OptStringArg(args, "mode", string(imageUnderstandModeFast)))))
	switch mode {
	case imageUnderstandModeFast, imageUnderstandModeAccurate:
		return mode, nil
	default:
		return "", fmt.Errorf("unsupported image understand mode %q; use fast or accurate", mode)
	}
}

func defaultImagePrompt(mode imageUnderstandMode) string {
	if mode == imageUnderstandModeAccurate {
		return defaultAccuratePrompt
	}
	return defaultFastImagePrompt
}

func imageUnderstandOutputTokens(args map[string]any) (int, error) {
	v, ok := args["max_tokens"]
	if !ok {
		return fastImageMaxTokens, nil
	}
	var n int
	switch value := v.(type) {
	case int:
		n = value
	case float64:
		if value < 1 || value > 4096 || value != float64(int(value)) {
			return 0, fmt.Errorf("max_tokens must be an integer between 1 and 4096")
		}
		n = int(value)
	default:
		return 0, fmt.Errorf("max_tokens must be an integer between 1 and 4096")
	}
	if n < 1 || n > 4096 {
		return 0, fmt.Errorf("max_tokens must be an integer between 1 and 4096")
	}
	return n, nil
}

type understandingImage struct {
	data      []byte
	mediaType string
}

func (t *ImageUnderstandTool) resolveUnderstandModel(args map[string]any, mode imageUnderstandMode) (config.ImageUnderstandConfig, error) {
	name := strings.TrimSpace(toolutil.OptStringArg(args, "model", ""))
	if name != "" {
		for _, model := range t.models {
			if model.Name == name {
				return model, nil
			}
		}
		return config.ImageUnderstandConfig{}, fmt.Errorf("image understand model %q is not configured", name)
	}
	if len(t.models) == 0 {
		return config.ImageUnderstandConfig{}, fmt.Errorf("no image understand models configured — add image_understand_models in ~/.nekocode/config.json")
	}
	for _, model := range t.models {
		if strings.EqualFold(strings.TrimSpace(model.Name), string(mode)) {
			return model, nil
		}
	}
	if mode == imageUnderstandModeAccurate {
		return config.ImageUnderstandConfig{}, fmt.Errorf("accurate image understanding model is not configured — add a model named %q to image_understand_models or pass model explicitly", imageUnderstandModeAccurate)
	}
	return t.models[0], nil
}

func readImageForUnderstanding(ctx context.Context, path string) ([]byte, string, error) {
	manager, ok := workspace.FromContext(ctx)
	if !ok {
		return nil, "", fmt.Errorf("invalid image path: workspace manager is unavailable")
	}
	safePath, _, allowed, err := manager.CheckReadLexical(path)
	if err != nil {
		return nil, "", fmt.Errorf("invalid image path: %w", err)
	}
	if !allowed {
		return nil, "", fmt.Errorf("invalid image path: path is outside configured workspaces")
	}
	return readImageFile(safePath)
}

func readImageFile(path string) ([]byte, string, error) {
	file, err := openImageFile(path)
	if err != nil {
		return nil, "", fmt.Errorf("open image: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, "", fmt.Errorf("stat image: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, "", fmt.Errorf("image path is not a regular file")
	}
	if info.Size() > maxImageUnderstandBytes {
		return nil, "", fmt.Errorf("image exceeds 20 MiB limit")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxImageUnderstandBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("read image: %w", err)
	}
	if len(data) == 0 {
		return nil, "", fmt.Errorf("image file is empty")
	}
	if len(data) > maxImageUnderstandBytes {
		return nil, "", fmt.Errorf("image exceeds 20 MiB limit")
	}
	mediaType := http.DetectContentType(data)
	if mediaType == "application/octet-stream" && len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP" {
		mediaType = "image/webp"
	}
	switch mediaType {
	case "image/png", "image/jpeg", "image/webp", "image/gif":
		return data, mediaType, nil
	default:
		return nil, "", fmt.Errorf("unsupported image type %q; use PNG, JPEG, WebP, or GIF", mediaType)
	}
}

func imageDigest(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func (t *ImageUnderstandTool) postJSON(ctx context.Context, endpoint string, headers map[string]string, body any, result any) error {
	resp, err := t.post(ctx, endpoint, headers, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(result)
}

func (t *ImageUnderstandTool) post(ctx context.Context, endpoint string, headers map[string]string, body any) (*http.Response, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	client := t.client
	if client == nil {
		client = newImageUnderstandHTTPClient()
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("image model request failed (%s): %s", resp.Status, strings.TrimSpace(string(data)))
	}
	return resp, nil
}

func newImageUnderstandHTTPClient() *http.Client {
	client := toolutil.NewToolHTTPClient(120 * time.Second)
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return client
}

func encodedImage(data []byte) string { return base64.StdEncoding.EncodeToString(data) }

func imageUnderstandEndpoint(baseURL, protocol string) (string, error) {
	var err error
	baseURL, err = utilhttp.NormalizeSecureURL(baseURL)
	if err != nil {
		return "", err
	}
	if baseURL == "" {
		if protocol == "anthropic" {
			baseURL = "https://api.anthropic.com/v1"
		} else {
			baseURL = "https://api.openai.com/v1"
		}
	}
	if protocol == "anthropic" {
		return utilurl.JoinURLPathWithVersion(baseURL, "v1", "messages"), nil
	}
	return utilurl.JoinURLPathWithVersion(baseURL, "v1", "chat/completions"), nil
}
