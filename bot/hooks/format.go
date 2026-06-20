package hooks

import (
	"fmt"
	"strings"
)

func FormatHints(hints []Hint) string {
	if len(hints) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("<hints>\n")
	for _, h := range hints {
		sev := h.Severity
		if sev == "" {
			sev = "info"
		}
		fmt.Fprintf(&b, "  <hint type=%q severity=%q>\n    %s\n  </hint>\n", h.Type, sev, h.Content)
	}
	b.WriteString("</hints>")
	return b.String()
}
