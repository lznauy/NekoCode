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
	f := newFile(path)
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

func newFile(path string) *File {
	f := &File{path: path}
	f.fieldMap = map[string]*string{
		"TechStack":      &f.TechStack,
		"ActiveGoals":    &f.ActiveGoals,
		"CompletedTasks": &f.CompletedTasks,
		"ArchMap":        &f.ArchMap,
		"Preferences":    &f.Preferences,
	}
	return f
}
