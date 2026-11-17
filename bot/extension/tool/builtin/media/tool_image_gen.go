package media

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"sync"
	"time"

	"nekocode/bot/config"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/toolutil"
)

type ImageGenTool struct {
	toolutil.SequentialSafeTool
	client       *http.Client
	models       []config.ImageGenConfig
	modelsMu     sync.RWMutex
	pendingMu    sync.Mutex
	pending      map[string]imageTask
	pendingOrder []string
}

func NewImageGenTool(models []config.ImageGenConfig) *ImageGenTool {
	return &ImageGenTool{
		client: toolutil.NewToolHTTPClient(120 * time.Second),
		models: slices.Clone(models),
	}
}

// SetModels refreshes new-generation configuration while preserving receipts
// for accepted tasks, which retain their original configuration snapshot.
func (t *ImageGenTool) SetModels(models []config.ImageGenConfig) {
	t.modelsMu.Lock()
	defer t.modelsMu.Unlock()
	t.models = slices.Clone(models)
}

func (t *ImageGenTool) configuredModels() []config.ImageGenConfig {
	t.modelsMu.RLock()
	defer t.modelsMu.RUnlock()
	return slices.Clone(t.models)
}

func (t *ImageGenTool) Name() string { return "image_gen" }

func (t *ImageGenTool) Description() string {
	return "Generate images from text prompts using configured text-to-image models. Images are validated before saving (PNG/JPEG/GIF/WebP, at most 20 MiB and 25 million pixels), and local paths are returned. New generation requires prompt. Use the model enum to select a configured model, or omit it for the default. After a submitted task fails during polling or download, use its returned resume_id only; never resubmit the prompt automatically."
}

func (t *ImageGenTool) Parameters() []core.Parameter {
	var names []string
	for _, model := range t.configuredModels() {
		names = append(names, model.Name)
	}
	return []core.Parameter{
		{Name: "resume_id", Type: "string", Description: "Opaque receipt returned after an accepted generation task. Supply only resume_id to query/download the same task without submitting or paying for another generation. Available within this process; never invent IDs."},
		{Name: "prompt", Type: "string", Required: false, Description: "Text prompt with non-whitespace content; recommended ≤120 Unicode characters, maximum 800. Required for new generation."},
		{Name: "output_dir", Type: "string", Required: false, Description: "Directory to save generated images. An explicit directory must be workspace-authorized. Defaults to ~/.nekocode/images."},
		{Name: "width", Type: "integer", Required: false, Description: "Image width, 1–32768. Must also set height. Omit both for 1328×1328; at most 25 million pixels total."},
		{Name: "height", Type: "integer", Required: false, Description: "Image height, 1–32768. Must also set width."},
		{Name: "seed", Type: "integer", Required: false, Description: "Random seed, -1 for random (default), otherwise a non-negative integer."},
		{Name: "model", Type: "string", Required: false, Enum: names, Description: "Configured model name from this enum. Uses the first configured model if omitted."},
	}
}

func (t *ImageGenTool) Execute(ctx context.Context, args map[string]any) (core.ToolOutput, error) {
	if _, supplied := args["resume_id"]; supplied {
		return t.resumeImage(ctx, args)
	}
	prompt, err := toolutil.RequireStringArg(args, "prompt")
	if err != nil {
		return core.ToolOutput{}, err
	}
	if err := validateGenerationArgs(prompt, args); err != nil {
		return core.ToolOutput{}, err
	}

	cfg, err := t.resolveModel(args)
	if err != nil {
		return core.ToolOutput{}, err
	}
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
