package memory

import (
	"fmt"
	"strings"
)

// Append adds a line to a section. Used by /remember.
func (f *File) Append(section, line string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	key := f.resolveSection(section)
	if key == "" {
		return fmt.Errorf("unknown section: %s (valid: tech-stack, goals, completed, architecture, preferences)", section)
	}

	current := f.getField(key)
	if strings.TrimSpace(current) == "" {
		f.setField(key, "- "+line)
	} else {
		f.setField(key, current+"\n- "+line)
	}
	return nil
}
