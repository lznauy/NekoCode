package memory

import "strings"

func (f *File) parse(data string) {
	current := ""
	for line := range strings.SplitSeq(data, "\n") {
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
