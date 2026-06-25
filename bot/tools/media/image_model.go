package media

import (
	"fmt"
	"os"
	"path/filepath"

	"nekocode/bot/config"
	"nekocode/bot/tools"
	"nekocode/bot/tools/toolhelpers"
	"nekocode/common"
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

func resolveOutputDir(args map[string]any) (string, error) {
	outputDir := toolhelpers.OptStringArg(args, "output_dir", "")
	if outputDir == "" {
		outputDir = filepath.Join(common.NekocodeHome(), "images")
	}
	safeOutputDir, err := tools.ValidatePath(outputDir)
	if err != nil {
		return "", fmt.Errorf("invalid output_dir: %w", err)
	}
	if err := os.MkdirAll(safeOutputDir, 0o755); err != nil {
		return "", fmt.Errorf("create output_dir: %w", err)
	}
	return safeOutputDir, nil
}
