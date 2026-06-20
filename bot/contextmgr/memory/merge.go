package memory

import "strings"

// MergeFromCompaction updates memory from Auto-Compaction output.
// newFacts are lines extracted from the summarizer's <key-facts> block;
// goal is the updated current goal; archMap entries are derived from facts.
func (f *File) MergeFromCompaction(newFacts []string, goal string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if strings.TrimSpace(goal) != "" {
		f.ActiveGoals = "- " + goal
	}

	for _, fact := range newFacts {
		fact = strings.TrimSpace(fact)
		if fact == "" || containsLine(f.ArchMap, fact) {
			continue
		}
		if strings.TrimSpace(f.ArchMap) == "" {
			f.ArchMap = "- " + fact
		} else {
			f.ArchMap += "\n- " + fact
		}
	}
}

func containsLine(haystack, needle string) bool {
	needle = cleanListLine(needle)
	for line := range strings.SplitSeq(haystack, "\n") {
		if cleanListLine(line) == needle {
			return true
		}
	}
	return false
}

func cleanListLine(line string) string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "- ")
	line = strings.TrimPrefix(line, "* ")
	line = strings.TrimPrefix(line, "• ")
	return line
}
