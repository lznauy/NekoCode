package pathutil

import (
	"fmt"
	"os"
	"path/filepath"

	"nekocode/bot/tools/textutil"
)

// ValidatePath resolves path against the current working directory.
func ValidatePath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("path resolution failed: %w", err)
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		parent := filepath.Dir(abs)
		realParent, pErr := filepath.EvalSymlinks(parent)
		if pErr != nil {
			return abs, nil
		}
		return filepath.Join(realParent, filepath.Base(abs)), nil
	}
	return real, nil
}

// NormalizePathKey normalizes a path for use as a cache key.
func NormalizePathKey(path string) string {
	if resolved, err := ValidatePath(path); err == nil {
		return resolved
	}
	abs, _ := filepath.Abs(path)
	return abs
}

func ReadNormalizedFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return textutil.NormalizeText(string(data)), nil
}

func ReadSafeFile(path string) ([]byte, error) {
	safePath, err := ValidatePath(path)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(safePath)
}
