package memory

import "strings"

// Build returns the formatted memory block for Layer 0 injection.
func (f *File) Build() string {
	f.mu.RLock()
	defer f.mu.RUnlock()

	var b strings.Builder
	b.WriteString("[Project Memory]\n")
	hasContent := false

	for _, key := range sectionOrder {
		content := strings.TrimSpace(f.getField(key))
		if content == "" {
			continue
		}
		header := sectionHeaders[key]
		b.WriteString(header)
		b.WriteString("\n")
		b.WriteString(content)
		b.WriteString("\n\n")
		hasContent = true
	}
	if !hasContent {
		return ""
	}
	return b.String()
}
