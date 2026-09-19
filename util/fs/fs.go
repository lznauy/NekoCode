// Package fs provides filesystem path and I/O helpers.
package fs

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// NekocodeHome returns the user-level ~/.nekocode directory path.
func NekocodeHome() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".nekocode")
}

// NekocodeLogDir returns the user-level ~/.nekocode/logs directory path.
// All runtime log output (debug logs, panic logs) should live here, not /tmp.
func NekocodeLogDir() string {
	return filepath.Join(NekocodeHome(), "logs")
}

// NekocodeDataDir returns a user-level ~/.nekocode/<subdir> data directory path.
// Used for runtime artifacts that are not logs (edit undo snapshots, exports, ...).
func NekocodeDataDir(subdir string) string {
	return filepath.Join(NekocodeHome(), subdir)
}

// NekocodeDirs returns the project-level and user-level .nekocode/<subdir> directories.
func NekocodeDirs(subdir string) []string {
	cwd, _ := os.Getwd()
	return NekocodeDirsAt(cwd, subdir)
}

// NekocodeDirsAt pins project discovery to an instance's root directory.
func NekocodeDirsAt(root, subdir string) []string {
	var dirs []string
	if root != "" {
		dirs = append(dirs, filepath.Join(root, ".nekocode", subdir))
	}
	dirs = append(dirs, filepath.Join(NekocodeHome(), subdir))
	return dirs
}

// WriteFileWithDir creates parent directories and writes data to path.
func WriteFileWithDir(path string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, data, perm); err != nil {
		return err
	}
	return os.Chmod(path, perm)
}

// ReadJSONFile reads a JSON file and unmarshals it into T.
func ReadJSONFile[T any](path string) (T, error) {
	var zero T
	data, err := os.ReadFile(path)
	if err != nil {
		return zero, err
	}
	var result T
	if err := json.Unmarshal(data, &result); err != nil {
		return zero, err
	}
	return result, nil
}
