package memory

import (
	"os"
	"path/filepath"

	"nekocode/common"
)

// DefaultPath returns the default memory file path.
func DefaultPath() string {
	return filepath.Join(common.NekocodeHome(), "memory.md")
}

// Load reads the memory file from disk. Returns an empty File if none exists.
func Load(path string) (*File, error) {
	f := &File{path: path}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return f, nil
		}
		return nil, err
	}
	f.parse(string(data))
	return f, nil
}
