package media

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"nekocode/bot/config"
	"nekocode/bot/extension/tool/runtime/authority"
	"nekocode/bot/extension/tool/runtime/toolutil"
	"nekocode/util/fs"
)

func (t *ImageGenTool) resolveModel(args map[string]any) (config.ImageGenConfig, error) {
	models := t.configuredModels()
	if raw, supplied := args["model"]; supplied {
		name, ok := raw.(string)
		if !ok || strings.TrimSpace(name) == "" {
			return config.ImageGenConfig{}, fmt.Errorf("model must be a nonempty configured model name; omit it to use the default")
		}
		var names []string
		for _, m := range models {
			names = append(names, m.Name)
			if m.Name == name {
				return m, nil
			}
		}
		return config.ImageGenConfig{}, fmt.Errorf("unknown image model %q; available models: %s. Omit model to use the default", name, strings.Join(names, ", "))
	}
	if len(models) > 0 {
		return models[0], nil
	}
	return config.ImageGenConfig{}, nil
}

func resolveOutputDir(ctx context.Context, args map[string]any) (string, error) {
	if _, supplied := args["output_dir"]; supplied {
		value, err := toolutil.RequireStringArg(args, "output_dir")
		if err != nil || strings.TrimSpace(value) == "" {
			return "", fmt.Errorf("output_dir must be a nonempty directory path; omit it for the managed default")
		}
	}
	outputDir := toolutil.OptStringArg(args, "output_dir", "")
	explicit := outputDir != ""
	if outputDir == "" {
		outputDir = filepath.Join(fs.NekocodeHome(), "images")
	}
	var safeOutputDir string
	var err error
	if explicit {
		safeOutputDir, err = toolutil.ValidatePathWritableContext(ctx, outputDir)
	} else {
		safeOutputDir, err = toolutil.ValidatePath(outputDir)
	}
	if err != nil {
		return "", fmt.Errorf("invalid output_dir: %w", err)
	}
	if err := authority.CheckWritable(safeOutputDir); err != nil {
		return "", fmt.Errorf("invalid output_dir: %w", err)
	}
	if err := os.MkdirAll(safeOutputDir, 0o755); err != nil {
		return "", fmt.Errorf("create output_dir: %w", err)
	}
	return safeOutputDir, nil
}
