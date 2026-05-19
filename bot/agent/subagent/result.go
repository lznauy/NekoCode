package subagent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Status int

const (
	StatusCompleted Status = iota
	StatusFailed
	StatusPartial
)

type classification int

const (
	classPass        classification = iota
	classWarn
	classUnavailable
)

const largeResultThreshold = 2048
const largeResultPreviewLen = 1200

type Result struct {
	Status         Status
	Content        string
	KeyFiles       []string
	FilesChanged   []string
	Issues         []string
	TotalTokens    int
	ToolUseCount   int
	DurationMs     int64
	classification classification
}

func FormatResult(r *Result, compact bool) string {
	if len(r.Content) > largeResultThreshold {
		return formatLargeResult(r)
	}
	return formatResult(r, compact)
}

func formatResult(r *Result, compact bool) string {
	var b strings.Builder
	b.WriteString("<subagent-result>\n")
	if r.Content != "" {
		fmt.Fprintf(&b, "  <result>%s</result>\n", escape(r.Content))
	}
	if len(r.KeyFiles) > 0 {
		fmt.Fprintf(&b, "  <key-files>%s</key-files>\n", escape(strings.Join(r.KeyFiles, ", ")))
	}
	if len(r.FilesChanged) > 0 {
		fmt.Fprintf(&b, "  <files-changed>%s</files-changed>\n", escape(strings.Join(r.FilesChanged, ", ")))
	}
	if len(r.Issues) > 0 {
		fmt.Fprintf(&b, "  <issues>%s</issues>\n", escape(strings.Join(r.Issues, "; ")))
	}
	if !compact && (r.TotalTokens > 0 || r.ToolUseCount > 0 || r.DurationMs > 0) {
		b.WriteString("  <usage>\n")
		if r.TotalTokens > 0 {
			fmt.Fprintf(&b, "    <total_tokens>%d</total_tokens>\n", r.TotalTokens)
		}
		if r.ToolUseCount > 0 {
			fmt.Fprintf(&b, "    <tool_uses>%d</tool_uses>\n", r.ToolUseCount)
		}
		if r.DurationMs > 0 {
			fmt.Fprintf(&b, "    <duration_ms>%d</duration_ms>\n", r.DurationMs)
		}
		b.WriteString("  </usage>\n")
	}
	if r.classification == classWarn {
		b.WriteString("  <classification>warn</classification>\n")
	}
	b.WriteString("</subagent-result>")
	if r.classification == classWarn {
		return "SECURITY WARNING: This sub-agent performed actions that may violate security policy.\n\n" + b.String()
	}
	return b.String()
}

func formatLargeResult(r *Result) string {
	filename := fmt.Sprintf("nekocode-subagent-%d.txt", time.Now().UnixNano())
	path := filepath.Join(os.TempDir(), filename)
	full := formatResult(r, false)
	if err := os.WriteFile(path, []byte(full), 0644); err != nil {
		return "<!-- Warning: failed to persist large result: " + escape(err.Error()) + " -->\n" + full
	}
	preview := r.Content
	if len(preview) > largeResultPreviewLen {
		preview = preview[:largeResultPreviewLen]
	}
	var b strings.Builder
	fmt.Fprintf(&b, "<subagent-result persisted=\"true\" file=%q>\n", path)
	fmt.Fprintf(&b, "  <result-preview>%s</result-preview>\n", escape(preview))
	if r.TotalTokens > 0 || r.ToolUseCount > 0 || r.DurationMs > 0 {
		b.WriteString("  <usage>\n")
		if r.TotalTokens > 0 {
			fmt.Fprintf(&b, "    <total_tokens>%d</total_tokens>\n", r.TotalTokens)
		}
		if r.ToolUseCount > 0 {
			fmt.Fprintf(&b, "    <tool_uses>%d</tool_uses>\n", r.ToolUseCount)
		}
		if r.DurationMs > 0 {
			fmt.Fprintf(&b, "    <duration_ms>%d</duration_ms>\n", r.DurationMs)
		}
		b.WriteString("  </usage>\n")
	}
	if r.classification == classWarn {
		b.WriteString("  <classification>warn</classification>\n")
	}
	b.WriteString("</subagent-result>")
	return b.String()
}

func parseStructuredOutput(raw string) (content string, keyFiles, filesChanged, issues []string) {
	content = extractField(raw, "Result:")
	keyFiles = extractList(raw, "Key files:")
	filesChanged = extractList(raw, "Files changed:")
	issues = extractList(raw, "Issues:")
	return
}

func extractField(text, label string) string {
	idx := findLabel(text, label)
	if idx < 0 {
		return ""
	}
	rest := text[idx+len(label):]
	rest = strings.TrimLeft(rest, " \t")
	if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
		rest = rest[:nl]
	}
	return strings.TrimSpace(rest)
}

func extractList(text, label string) []string {
	idx := findLabel(text, label)
	if idx < 0 {
		return nil
	}
	var items []string
	for _, line := range strings.Split(text[idx+len(label):], "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, "- ")
		line = strings.TrimPrefix(line, "* ")
		if line == "" || isLabelLine(line) {
			break
		}
		lower := strings.ToLower(line)
		if lower != "n/a" && lower != "none" && lower != "nil" {
			items = append(items, line)
		}
	}
	return items
}

func findLabel(text, label string) int {
	lower := strings.ToLower(text)
	ll := strings.ToLower(label)
	idx := strings.Index(lower, ll)
	if idx < 0 {
		return -1
	}
	if idx > 0 && text[idx-1] != '\n' {
		for i := idx - 1; i >= 0; i-- {
			if text[i] == '\n' {
				break
			}
			if text[i] != ' ' && text[i] != '\t' {
				return -1
			}
		}
	}
	return idx
}

func isLabelLine(line string) bool {
	for _, l := range []string{"scope:", "result:", "key files:", "files changed:", "issues:"} {
		if strings.HasPrefix(strings.ToLower(line), l) {
			return true
		}
	}
	return false
}

func escape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	return s
}
