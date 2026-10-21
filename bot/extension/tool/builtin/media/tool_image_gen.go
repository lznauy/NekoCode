package media

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"nekocode/bot/config"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/toolutil"
)

type ImageGenTool struct {
	toolutil.SequentialSafeTool
	client *http.Client
	models []config.ImageGenConfig
}

func NewImageGenTool(models []config.ImageGenConfig) *ImageGenTool {
	return &ImageGenTool{
		client: toolutil.NewToolHTTPClient(120 * time.Second),
		models: models,
	}
}

func (t *ImageGenTool) Name() string { return "image_gen" }

func (t *ImageGenTool) Description() string {
	return "Generate images from text prompts using configured text-to-image models. Images are saved to local files and links are returned."
}

func (t *ImageGenTool) Parameters() []core.Parameter {
	return []core.Parameter{
		{Name: "prompt", Type: "string", Required: true, Description: "Chinese or English text prompt for image generation, recommended ≤120 chars, max 800"},
		{Name: "output_dir", Type: "string", Required: false, Description: "Directory to save generated images. An explicit directory must be workspace-authorized. Defaults to ~/.nekocode/images."},
		{Name: "width", Type: "integer", Required: false, Description: "Image width, default 1328. Must also set height."},
		{Name: "height", Type: "integer", Required: false, Description: "Image height, default 1328. Must also set width."},
		{Name: "seed", Type: "integer", Required: false, Description: "Random seed, -1 for random (default)"},
		{Name: "model", Type: "string", Required: false, Description: "Image gen model name from config. Uses first configured model if omitted."},
	}
}

func (t *ImageGenTool) Execute(ctx context.Context, args map[string]any) (core.ToolOutput, error) {
	prompt, err := toolutil.RequireStringArg(args, "prompt")
	if err != nil {
		return core.ToolOutput{}, err
	}

	cfg := t.resolveModel(args)
	if cfg.Name == "" {
		return core.ToolOutput{}, fmt.Errorf("no image gen models configured — add image_gen_models in ~/.nekocode/config.json")
	}
	outputDir, err := resolveOutputDir(ctx, args)
	if err != nil {
		return core.ToolOutput{}, err
	}

	switch cfg.Provider {
	case "jimeng":
		toolText, toolErr := t.executeJimeng(ctx, cfg, prompt, outputDir, args)
		return core.ToolOutput{Text: toolText}, toolErr
	default:
		return core.ToolOutput{}, fmt.Errorf("unsupported image gen provider: %s", cfg.Provider)
	}
}
