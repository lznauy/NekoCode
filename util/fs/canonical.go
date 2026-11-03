package fs

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// CanonicalPath resolves existing ancestors, including for not-yet-created paths.
func CanonicalPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	var missing []string
	for candidate := abs; ; candidate = filepath.Dir(candidate) {
		real, err := filepath.EvalSymlinks(candidate)
		if err == nil {
			for i := len(missing) - 1; i >= 0; i-- {
				real = filepath.Join(real, missing[i])
			}
			return filepath.Clean(real), nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		if _, statErr := os.Lstat(candidate); statErr == nil {
			return "", fmt.Errorf("unresolved symbolic link: %s", candidate)
		}
		if filepath.Dir(candidate) == candidate {
			return "", err
		}
		missing = append(missing, filepath.Base(candidate))
	}
}
