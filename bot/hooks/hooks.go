package hooks

import (
	"fmt"
	"strings"
)

type Hint struct {
	Type     string
	Severity string // info, warning, critical
	Content  string
}

type StopReason int

const (
	StopCompleted   StopReason = iota
	StopInterrupted
	StopFormatError
)

func (s StopReason) String() string {
	switch s {
	case StopCompleted:
		return "completed"
	case StopInterrupted:
		return "interrupted"
	case StopFormatError:
		return "format_error"
	}
	return "unknown"
}

func FormatHints(hints []Hint) string {
	if len(hints) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("<hints>\n")
	for _, h := range hints {
		fmt.Fprintf(&b, "  <hint type=%q severity=%q>%s</hint>\n", h.Type, h.Severity, h.Content)
	}
	b.WriteString("</hints>")
	return b.String()
}
