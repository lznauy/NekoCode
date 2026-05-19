// Package memory manages the persistent project memory file.
// Memory is injected into Layer 0 (immutable prefix) and only changes
// on explicit /remember commands or Layer 5 Auto-Compaction.
//
// Five sections, stored as a markdown file:
//
//	## Tech Stack           — languages, frameworks, infrastructure
//	## Active Goals         — current tasks in progress
//	## Completed Tasks      — milestones achieved
//	## Key Architecture Map — component → responsibility mappings
//	## User Preferences     — explicit user rules and preferences
package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// File is the in-memory representation of the memory file.
type File struct {
	mu       sync.RWMutex
	path     string

	TechStack      string
	ActiveGoals    string
	CompletedTasks string
	ArchMap        string
	Preferences    string
}

// sectionHeaders maps section names to their markdown headers.
var sectionHeaders = map[string]string{
	"TechStack":      "## Tech Stack",
	"ActiveGoals":    "## Active Goals",
	"CompletedTasks": "## Completed Tasks",
	"ArchMap":        "## Key Architecture Map",
	"Preferences":    "## User Preferences",
}

// DefaultPath returns the default memory file path.
func DefaultPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".nekocode", "memory.md")
}

// Load reads the memory file from disk. Returns an empty File if none exists.
func Load(path string) (*File, error) {
	f := &File{path: path}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return f, nil // fresh start
		}
		return nil, err
	}
	f.parse(string(data))
	return f, nil
}

// Save writes the memory file to disk.
func (f *File) Save() error {
	f.mu.RLock()
	defer f.mu.RUnlock()

	dir := filepath.Dir(f.path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

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
	return os.WriteFile(f.path, []byte(b.String()), 0644)
}

// Build returns the formatted memory block for Layer 0 injection.
func (f *File) Build() string {
	f.mu.RLock()
	defer f.mu.RUnlock()

	var b strings.Builder
	b.WriteString("[Project Memory]\n")
	hasContent := false

	for _, key := range []string{"TechStack", "ActiveGoals", "CompletedTasks", "ArchMap", "Preferences"} {
		content := strings.TrimSpace(f.getField(key))
		if content == "" {
			continue
		}
		header := sectionHeaders[key]
		b.WriteString(header + "\n")
		b.WriteString(content + "\n\n")
		hasContent = true
	}
	if !hasContent {
		return ""
	}
	return b.String()
}

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

// MergeFromCompaction updates memory from Auto-Compaction output.
// newFacts are lines extracted from the summarizer's <key-facts> block;
// goal is the updated current goal; archMap entries are derived from facts.
func (f *File) MergeFromCompaction(newFacts []string, goal string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	// Update goal.
	if strings.TrimSpace(goal) != "" {
		f.ActiveGoals = "- " + goal
	}

	// Merge new facts into architecture map.
	for _, fact := range newFacts {
		fact = strings.TrimSpace(fact)
		if fact == "" {
			continue
		}
		if strings.Contains(f.ArchMap, fact) {
			continue
		}
		if strings.TrimSpace(f.ArchMap) == "" {
			f.ArchMap = "- " + fact
		} else {
			f.ArchMap += "\n- " + fact
		}
	}
}

// -- internal ----------------------------------------------------------

func (f *File) parse(data string) {
	current := ""
	for _, line := range strings.Split(data, "\n") {
		trimmed := strings.TrimSpace(line)
		for key, header := range sectionHeaders {
			if trimmed == header {
				current = key
				break
			}
		}
		if current != "" && !strings.HasPrefix(trimmed, "##") {
			existing := f.getField(current)
			if existing == "" {
				f.setField(current, trimmed)
			} else {
				f.setField(current, existing+"\n"+trimmed)
			}
		}
	}
}

func (f *File) resolveSection(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "tech-stack", "techstack", "tech":
		return "TechStack"
	case "goals", "active-goals", "goal":
		return "ActiveGoals"
	case "completed", "completed-tasks", "done":
		return "CompletedTasks"
	case "architecture", "arch", "architecture-map":
		return "ArchMap"
	case "preferences", "prefs", "user-preferences":
		return "Preferences"
	default:
		return ""
	}
}

func (f *File) getField(key string) string {
	switch key {
	case "TechStack":
		return f.TechStack
	case "ActiveGoals":
		return f.ActiveGoals
	case "CompletedTasks":
		return f.CompletedTasks
	case "ArchMap":
		return f.ArchMap
	case "Preferences":
		return f.Preferences
	}
	return ""
}

func (f *File) setField(key, value string) {
	switch key {
	case "TechStack":
		f.TechStack = value
	case "ActiveGoals":
		f.ActiveGoals = value
	case "CompletedTasks":
		f.CompletedTasks = value
	case "ArchMap":
		f.ArchMap = value
	case "Preferences":
		f.Preferences = value
	}
}
