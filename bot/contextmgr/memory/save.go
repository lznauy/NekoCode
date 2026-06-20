package memory

import (
	"strings"

	"nekocode/common"
)

// Save writes the memory file to disk.
func (f *File) Save() error {
	f.mu.RLock()
	defer f.mu.RUnlock()

	var b strings.Builder
	for _, key := range []string{"TechStack", "ActiveGoals", "CompletedTasks", "ArchMap", "Preferences"} {
		header := sectionHeaders[key]
		content := f.getField(key)
		b.WriteString(header + "\n")
		if strings.TrimSpace(content) == "" {
			b.WriteString("\n")
		} else {
			b.WriteString(content + "\n\n")
		}
	}
	return common.WriteFileWithDir(f.path, []byte(b.String()), 0o644)
}
