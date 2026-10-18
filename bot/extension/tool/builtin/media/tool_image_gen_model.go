package media

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"nekocode/bot/config"
	"nekocode/bot/extension/tool/runtime/toolutil"
	"nekocode/util/fs"
)

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

func resolveOutputDir(ctx context.Context, args map[string]any) (string, error) {
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
	if err := os.MkdirAll(safeOutputDir, 0o755); err != nil {
		return "", fmt.Errorf("create output_dir: %w", err)
	}
	return safeOutputDir, nil
}
