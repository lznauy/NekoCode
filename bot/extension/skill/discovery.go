package skill

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"nekocode/common"
)

// DefaultDirs returns the default skill directories (project + user).
func DefaultDirs() []string {
	return common.NekocodeDirs("skills")
}

func discoverSkills(dirs []string) []string {
	seen := make(map[string]bool)
	var paths []string
	for _, dir := range dirs {
		absDir, err := filepath.Abs(dir)
		if err != nil {
			continue
		}
		filepath.Walk(absDir, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			if strings.EqualFold(info.Name(), "skill.md") {
				abs, _ := filepath.Abs(path)
				if !seen[abs] {
					seen[abs] = true
					paths = append(paths, abs)
				}
				return filepath.SkipDir
			}
			return nil
		})
	}
	sort.Strings(paths)
	return paths
}
