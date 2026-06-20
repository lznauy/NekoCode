package editdsl

import (
	"fmt"
	"sort"
	"strings"
)

// ApplyEdits applies a list of hunks to the given text and returns the result.
func ApplyEdits(text string, hunks []Hunk, resolver BlockResolver, path string) (*ApplyResult, error) {
	if len(hunks) == 0 {
		return &ApplyResult{Text: text}, nil
	}

	lines := strings.Split(NormalizeToLF(text), "\n")

	// Strip auto-prefix sentinels and collect warnings.
	// Clone payload slices before mutation to avoid modifying caller data.
	var autoprefixWarnings []string
	for hi := range hunks {
		var payloadCopied bool
		for pi := range hunks[hi].Payload {
			if rest, ok := strings.CutPrefix(hunks[hi].Payload[pi], autoprefixSentinel); ok {
				if !payloadCopied {
					payload := make([]string, len(hunks[hi].Payload))
					copy(payload, hunks[hi].Payload)
					hunks[hi].Payload = payload
					payloadCopied = true
				}
				hunks[hi].Payload[pi] = rest
				if len(autoprefixWarnings) == 0 {
					autoprefixWarnings = append(autoprefixWarnings,
						"auto-prefixed bare body row(s) with '+': the model emitted body rows without the required '+' prefix. "+
							"Body rows must be '+TEXT' literal lines, not bare content. NekoCode added the prefix automatically.")
				}
			}
		}
	}

	// Resolve block hunks to concrete ranges.
	resolved, err := resolveBlockHunks(hunks, lines, resolver, path)
	if err != nil {
		return nil, err
	}
	hunks = resolved

	// Drop delete hunks targeting the trailing newline sentinel — a phantom
	// line that \n-split produces when the file ends with a newline. Deleting
	// it would only strip the file's final newline; the intended operation is
	// always to delete the last concrete line, which inclusive ranges achieve
	// naturally. Mirrors oh-my-pi's dropTrailingPhantomDeletes.
	if len(lines) > 1 && lines[len(lines)-1] == "" {
		phantomLine := len(lines)
		filtered := hunks[:0]
		for _, h := range hunks {
			if h.Kind == HunkDelete && h.Start == phantomLine && h.End == phantomLine {
				continue
			}
			filtered = append(filtered, h)
		}
		hunks = filtered
	}

	// Validate all hunk ranges.
	for _, h := range hunks {
		if h.Kind == HunkInsert && (h.Cursor == CursorHead || h.Cursor == CursorTail) {
			continue
		}
		if h.Start < 1 || h.Start > len(lines) {
			return nil, fmt.Errorf("hunk start line %d out of range [1..%d]", h.Start, len(lines))
		}
		if h.Kind != HunkInsert {
			if h.End < 1 || h.End > len(lines) {
				return nil, fmt.Errorf("hunk end line %d out of range [1..%d]", h.End, len(lines))
			}
			if h.End < h.Start {
				return nil, fmt.Errorf("hunk end line %d precedes start line %d", h.End, h.Start)
			}
		}
	}

	// Build indexed hunks preserving original order.
	type indexedHunk struct {
		Hunk
		idx int
	}
	indexed := make([]indexedHunk, len(hunks))
	for i, h := range hunks {
		indexed[i] = indexedHunk{h, i}
	}
	sorted := make([]Hunk, len(indexed))
	for i, ih := range indexed {
		sorted[i] = ih.Hunk
	}

	// After-insert landing shift: slide after-insert hunks past trailing
	// structural closers when the body indentation is shallower than the anchor.
	// Run BEFORE sorting so the shifted Start values participate in the
	// bottom-up sort order — otherwise a landing shift on an insert-after hunk
	// can move it into another hunk's target range after the sort is fixed.
	sorted, landingWarnings := RepairAfterInsertLandings(sorted, lines)
	autoprefixWarnings = append(autoprefixWarnings, landingWarnings...)

	// Copy shifted Start values back to indexed for re-sorting.
	for i := range indexed {
		indexed[i].Hunk.Start = sorted[i].Start
	}

	// Sort hunks bottom-up for stable application.
	// Use original index as tiebreaker to ensure stable ordering for
	// head/tail inserts that share the same Start value (0).
	sort.Slice(indexed, func(i, j int) bool {
		a, b := indexed[i], indexed[j]
		aHead := a.Kind == HunkInsert && a.Cursor == CursorHead
		bHead := b.Kind == HunkInsert && b.Cursor == CursorHead
		aTail := a.Kind == HunkInsert && a.Cursor == CursorTail
		bTail := b.Kind == HunkInsert && b.Cursor == CursorTail

		// Head inserts sort before everything else.
		if aHead && !bHead {
			return true
		}
		if bHead && !aHead {
			return false
		}
		// Tail inserts sort after everything else.
		if aTail && !bTail {
			return false
		}
		if bTail && !aTail {
			return true
		}
		// Both are same special type (both head or both tail) — preserve
		// patch order using original index as tiebreaker.
		if a.Start == b.Start {
			return a.idx < b.idx
		}
		// For regular hunks, sort descending by Start (bottom-up).
		return a.Start > b.Start
	})

	// Rebuild sorted from re-sorted indexed.
	sorted = make([]Hunk, len(indexed))
	for i, ih := range indexed {
		sorted[i] = ih.Hunk
	}

	// Track original line identity parallel to the content slice.
	// identities[i] = old 1-based line number at current position i,
	// or 0 for inserted lines. After all edits we build OldToNew from it.
	identities := make([]int, len(lines))
	for i := range identities {
		identities[i] = i + 1
	}

	// Apply hunks.
	firstChanged := len(lines) + 1
	var warnings []string

	for i := range sorted {
		h := &sorted[i]
		switch h.Kind {
		case HunkReplace:
			payload := h.Payload
			payload, w := repairBoundaries(lines, *h, payload)
			warnings = append(warnings, w...)

			start := h.Start - 1
			end := h.End

			payload = repairDelimiterBalance(lines[start:end], payload)

			newLines := make([]string, 0, len(lines)+len(payload)-(end-start))
			newLines = append(newLines, lines[:start]...)
			newLines = append(newLines, payload...)
			newLines = append(newLines, lines[end:]...)
			lines = newLines

			zeros := make([]int, len(payload))
			newIdentities := make([]int, 0, len(identities)+len(payload)-(end-start))
			newIdentities = append(newIdentities, identities[:start]...)
			newIdentities = append(newIdentities, zeros...)
			newIdentities = append(newIdentities, identities[end:]...)
			identities = newIdentities

			if h.Start < firstChanged {
				firstChanged = h.Start
			}
			// Update the hunk payload to reflect boundary repair so the
			// returned ResolvedHunks match what was actually applied.
			h.Payload = payload

		case HunkDelete:
			start := h.Start - 1
			end := h.End
			lines = append(lines[:start], lines[end:]...)
			identities = append(identities[:start], identities[end:]...)
			if h.Start < firstChanged {
				firstChanged = h.Start
			}

		case HunkInsert:
			var idx int
			switch h.Cursor {
			case CursorHead:
				idx = 0
			case CursorTail:
				idx = len(lines)
			case CursorBefore:
				idx = h.Start - 1
			case CursorAfter:
				idx = h.Start
			default:
				idx = h.Start - 1
			}
			if idx < 0 {
				idx = 0
			}
			if idx > len(lines) {
				idx = len(lines)
			}
			newLines := make([]string, 0, len(lines)+len(h.Payload))
			newLines = append(newLines, lines[:idx]...)
			newLines = append(newLines, h.Payload...)
			newLines = append(newLines, lines[idx:]...)
			lines = newLines

			zeros := make([]int, len(h.Payload))
			newIdentities := make([]int, 0, len(identities)+len(h.Payload))
			newIdentities = append(newIdentities, identities[:idx]...)
			newIdentities = append(newIdentities, zeros...)
			newIdentities = append(newIdentities, identities[idx:]...)
			identities = newIdentities

			if idx < firstChanged {
				firstChanged = idx + 1
			}
		}
	}

	if firstChanged > len(lines) {
		firstChanged = len(lines)
	}

	warnings = append(autoprefixWarnings, warnings...)

	// Collapse runs of 3+ blank lines to 2 — keeps code tidy.
	var collapsedBlanks int
	var removedIndices []int
	lines, collapsedBlanks, removedIndices = collapseExcessBlankLines(lines)
	// Remove collapsed indices from identities (in descending order).
	for i := len(removedIndices) - 1; i >= 0; i-- {
		idx := removedIndices[i]
		identities = append(identities[:idx], identities[idx+1:]...)
	}
	if collapsedBlanks > 0 {
		warnings = append(warnings, fmt.Sprintf("collapsed %d excess blank line(s)", collapsedBlanks))
	}

	// Build old-to-new line mapping from the identity array.
	oldToNew := make(map[int]int)
	for newIdx, orig := range identities {
		if orig > 0 {
			oldToNew[orig] = newIdx + 1
		}
	}

	// Rebuild resolvedHunks from sorted to include boundary repair changes.
	resolvedHunks := make([]Hunk, len(indexed))
	for i, ih := range indexed {
		// Find the hunk in sorted by matching the original index.
		// Since sorted may have different ordering, use the idx field
		// stored in indexed to map back to the original position.
		resolvedHunks[ih.idx] = sorted[i]
	}

	return &ApplyResult{
		Text:             strings.Join(lines, "\n"),
		FirstChangedLine: firstChanged,
		Warnings:         warnings,
		ResolvedHunks:    resolvedHunks,
		OldToNew:         oldToNew,
	}, nil
}
