package editdsl

import (
	"fmt"
	"regexp"
	"strings"
)

// unifiedDiffRe matches unified-diff hunk headers like "@@ -1,5 +1,6 @@".
var unifiedDiffRe = regexp.MustCompile(`^@@\s+[-+]?\d+,?\d*\s+[-+]?\d+,?\d*\s+@@`)

// detectApplyPatchContamination checks for common LLM mistakes:
// unified-diff headers, apply_patch tool confusion, bare line numbers, - rows.
func detectApplyPatchContamination(line string) *string {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return nil
	}
	if strings.HasPrefix(trimmed, "***") {
		msg := fmt.Sprintf("apply_patch format is not valid in editdsl. Use [PATH#TAG] for file sections, not %q.", trimmed)
		return &msg
	}
	if strings.HasPrefix(trimmed, "@@") {
		msg := "unified-diff hunk header (@@ ... @@) is not valid in editdsl. Use 'replace N..M:', 'delete N..M', or 'insert after N:'."
		return &msg
	}
	if isAllDigits(trimmed) {
		msg := fmt.Sprintf("hunk headers need a verb. Use 'replace %s..%s:' to replace, or 'delete %s' to delete.", trimmed, trimmed, trimmed)
		return &msg
	}
	if strings.Contains(trimmed, "..") {
		parts := strings.SplitN(trimmed, "..", 2)
		if isAllDigits(parts[0]) {
			endPart := strings.TrimSuffix(parts[1], ":")
			if isAllDigits(endPart) {
				msg := fmt.Sprintf("bare range %q is not valid. Use 'replace %s' or 'delete %s'.", trimmed, trimmed, strings.TrimSuffix(trimmed, ":"))
				return &msg
			}
		}
	}
	return nil
}

func isAllDigits(s string) bool {
	if len(s) == 0 {
		return false
	}
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}
