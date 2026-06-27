// Package editcore provides shared edit primitives: content hashing,
// line-ending normalization, and snapshots.
package editcore

import (
	"fmt"
	"hash/fnv"
	"strings"
)

// ComputeFileHash returns an 8-char uppercase hex tag from normalized text.
// The hash is stable across CRLF/LF differences and trailing whitespace.
// Uses full 32-bit FNV-1a (4 billion possible values) to minimize collision risk.
func ComputeFileHash(text string) string {
	norm := normalizeForHash(text)
	h := fnv.New32a()
	h.Write([]byte(norm))
	return fmt.Sprintf("%08X", h.Sum32())
}

// normalizeForHash canonicalizes text for hashing: CRLF→LF, strip trailing
// whitespace per line, strip final trailing newline.
func normalizeForHash(text string) string {
	text = NormalizeToLF(text)
	lines := strings.Split(text, "\n")
	var b strings.Builder
	for i, line := range lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(strings.TrimRight(line, " \t"))
	}
	// Strip trailing newline.
	return strings.TrimRight(b.String(), "\n")
}

// NormalizeToLF converts all line endings to LF.
func NormalizeToLF(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	return text
}

// DetectLineEnding returns the first line ending found ("\r\n" or "\n").
// Defaults to "\n" if no line endings are present.
func DetectLineEnding(text string) string {
	if strings.Contains(text, "\r\n") {
		return "\r\n"
	}
	return "\n"
}

// RestoreLineEndings converts LF back to the original line ending style.
func RestoreLineEndings(text, lineEnding string) string {
	if lineEnding == "\n" {
		return text
	}
	// First normalize to LF, then convert to target.
	text = NormalizeToLF(text)
	return strings.ReplaceAll(text, "\n", lineEnding)
}
