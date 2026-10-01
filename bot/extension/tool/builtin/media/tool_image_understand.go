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
	utilhttp "nekocode/util/http"
	utilurl "nekocode/util/url"
)

const (
	maxImageUnderstandBytes = 20 << 20
	defaultFastImagePrompt  = "Identify the task-relevant content in this image concisely. Include visible text, UI state, and errors when present."
	defaultAccuratePrompt   = "Analyze this image carefully and in detail. Include visible text, UI elements, errors, charts, and other task-relevant details. Clearly mark anything uncertain."
	approvedImageDigestArg  = "_nekocode_approved_image_sha256"
	unavailableImageDigest  = "unavailable"
	fastImageMaxTokens      = 1024
	accurateImageMaxTokens  = 4096
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
	return "Analyze a local image with a configured multimodal model. Use fast mode for ordinary screenshots and accurate mode for small text, OCR, charts, or complex reasoning."
}

func (t *ImageUnderstandTool) Parameters() []core.Parameter {
	return []core.Parameter{
		{Name: "path", Type: "string", Required: true, Description: "Path to a local PNG, JPEG, WebP, or GIF image"},
		{Name: "prompt", Type: "string", Required: false, Description: "Focused question or analysis instruction. A concise mode-specific prompt is used when omitted."},
		{Name: "mode", Type: "string", Required: false, Enum: []string{string(imageUnderstandModeFast), string(imageUnderstandModeAccurate)}, Description: "fast (default) resizes large images and limits output; accurate preserves the original image and uses the configured accurate model."},
		{Name: "model", Type: "string", Required: false, Description: "Image understanding model name from config. Uses the first configured model if omitted."},
	}
}

// PermissionPlan exposes the local file and remote destination in the
// confirmation shown before this data leaves the workspace.
func (t *ImageUnderstandTool) PermissionPlan(args map[string]any, workspaceRoot string) *core.PermissionRequest {
	args[approvedImageDigestArg] = unavailableImageDigest
	path := strings.TrimSpace(toolutil.OptStringArg(args, "path", ""))
	if path != "" {
		if !filepath.IsAbs(path) && workspaceRoot != "" {
			path = filepath.Join(workspaceRoot, path)
		}
		path = filepath.Clean(path)
		if data, _, err := readImageFile(path); err == nil {
			args[approvedImageDigestArg] = imageDigest(data)
		}
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
		Reason:       fmt.Sprintf("send local image %s to %s", path, destination),
		Capabilities: []string{core.CapNetOutbound},
		Scope:        "once",
		Details:      map[string]any{"workspace": workspaceRoot},
	}
}

func (t *ImageUnderstandTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	path, err := toolutil.RequireStringArg(args, "path")
	if err != nil {
		return "", err
	}
	mode, err := parseImageUnderstandMode(args)
	if err != nil {
		return "", err
	}
	cfg, err := t.resolveUnderstandModel(args, mode)
	if err != nil {
		return "", err
	}
	if approved, planned := args[approvedImageDigestArg].(string); planned && approved == unavailableImageDigest {
		return "", fmt.Errorf("image was unavailable during approval; choose a regular local file and approve it again")
	}
	imageData, mediaType, err := readImageForUnderstanding(ctx, path)
	if err != nil {
		return "", err
	}
	if approved, planned := args[approvedImageDigestArg].(string); planned && approved != imageDigest(imageData) {
		return "", fmt.Errorf("image changed or became unavailable after approval; review and approve it again")
	}
	imageData, mediaType, err = prepareImageForUnderstanding(imageData, mediaType, mode)
	if err != nil {
		return "", err
	}
	prompt := toolutil.OptStringArg(args, "prompt", defaultImagePrompt(mode))
	maxTokens := imageUnderstandMaxTokens(mode)

	protocol := strings.ToLower(strings.TrimSpace(cfg.Protocol))
	if protocol == "" {
		protocol = strings.ToLower(strings.TrimSpace(cfg.Provider))
		if protocol != "anthropic" {
			protocol = "openai"
		}
	}

	switch protocol {
	case "openai":
		return t.executeOpenAIUnderstanding(ctx, cfg, prompt, mediaType, imageData, maxTokens)
	case "anthropic":
		return t.executeAnthropicUnderstanding(ctx, cfg, prompt, mediaType, imageData, maxTokens)
	default:
		return "", fmt.Errorf("unsupported image understand protocol: %s", protocol)
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

func imageUnderstandMaxTokens(mode imageUnderstandMode) int {
	if mode == imageUnderstandModeAccurate {
		return accurateImageMaxTokens
	}
	return fastImageMaxTokens
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
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
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
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("image model request failed (%s): %s", resp.Status, strings.TrimSpace(string(data)))
	}
	if err := json.Unmarshal(data, result); err != nil {
		return fmt.Errorf("decode image model response: %w", err)
	}
	return nil
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
