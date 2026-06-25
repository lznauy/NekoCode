package editcore

import (
	"encoding/json"
	"path/filepath"
)

// ExtractPathsFromPatch pulls file paths from a JSON edit intent string.
func ExtractPathsFromPatch(patch any) []string {
	s, ok := patch.(string)
	if !ok || s == "" {
		return nil
	}
	var intent struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(s), &intent); err != nil || intent.Path == "" {
		return nil
	}
	return []string{intent.Path}
}

// ExtractFirstPathFromPatch extracts the basename of the first file path.
func ExtractFirstPathFromPatch(patch string) string {
	paths := ExtractPathsFromPatch(patch)
	if len(paths) > 0 {
		return filepath.Base(paths[0])
	}
	return ""
}
