// Package authority locates permission state outside model-writable projects.
package authority

import (
	"crypto/sha256"
	"fmt"
	"nekocode/util/fs"
	"os"
	"path/filepath"
	"strings"
)

// CanonicalPath resolves existing ancestors, including for not-yet-created paths.
func CanonicalPath(path string) (string, error) { return fs.CanonicalPath(path) }

// Root is the trusted user directory. A link replacing it is never followed.
func Root() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("permission authority home unavailable: %w", err)
	}
	if home == "" {
		return "", fmt.Errorf("permission authority home is empty")
	}
	home, err = CanonicalPath(home)
	if err != nil {
		return "", err
	}
	root := filepath.Join(home, ".nekocode", "permissions")
	real, err := CanonicalPath(root)
	if err != nil {
		return "", err
	}
	if root != real {
		return "", fmt.Errorf("permission authority directory must not be a symbolic link")
	}
	return root, nil
}

func StorePath(project string) (string, error) {
	root, err := Root()
	if err != nil {
		return "", err
	}
	project, err = CanonicalPath(project)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(project))
	path := filepath.Join(root, fmt.Sprintf("%x", sum), "permissions.json")
	real, err := CanonicalPath(path)
	if err != nil {
		return "", err
	}
	if real != path {
		return "", fmt.Errorf("permission authority store must not be a symbolic link")
	}
	return path, nil
}

// CheckWritable rejects the authority directory, its descendants and ancestors.
// Ancestors can rename or replace the trusted directory just as directly as files.
func CheckWritable(path string) error {
	root, err := Root()
	if err != nil {
		return err
	}
	path, err = CanonicalPath(path)
	if err != nil {
		return err
	}
	if within(path, root) || within(root, path) {
		return fmt.Errorf("write access overlaps trusted permission storage: %s", path)
	}
	return nil
}
func within(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
