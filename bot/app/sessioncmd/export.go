package sessioncmd

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"nekocode/common"
	"nekocode/llm/types"
)

// DefaultExportPath is the default context-export destination under ~/.nekocode/exports.
var DefaultExportPath = filepath.Join(common.NekocodeDataDir("exports"), "nekocode-context.json")

func ExportMessages(msgs []types.Message, path string) (string, error) {
	if path == "" {
		path = DefaultExportPath
	}
	data, err := json.MarshalIndent(msgs, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal context: %w", err)
	}
	if err := common.WriteFileWithDir(path, data, 0o644); err != nil {
		return "", fmt.Errorf("write file: %w", err)
	}
	return path, nil
}

func ExportFailed(err error) string {
	return fmt.Sprintf("Failed to %v", err)
}

func ExportSuccess(path string, msgCount int) string {
	return fmt.Sprintf("Context exported to %s (%d messages)", path, msgCount)
}
