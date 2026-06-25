// diff_hunks.go — hunk ordering and compact hunk diff rendering.

package edit

import (
	"fmt"
	"sort"
	"strings"

	"nekocode/bot/tools/editcore"
)

// sortHunksAscending sorts hunks in-place by ascending position for display.
// Head-cursor inserts sort first, tail-cursor inserts sort last.
func sortHunksAscending(hunks []editcore.Hunk) {
	sort.Slice(hunks, func(i, j int) bool {
		a, b := hunks[i], hunks[j]
		if a.Kind == editcore.HunkInsert && a.Cursor == editcore.CursorHead {
			return true
		}
		if b.Kind == editcore.HunkInsert && b.Cursor == editcore.CursorHead {
			return false
		}
		if a.Kind == editcore.HunkInsert && a.Cursor == editcore.CursorTail {
			return false
		}
		if b.Kind == editcore.HunkInsert && b.Cursor == editcore.CursorTail {
			return true
		}
		return a.Start < b.Start
	})
}

func formatHunkDiff(oldLines, newLines []string, hunks []editcore.Hunk, oldToNew map[int]int) string {
	type outLine struct {
		prefix string
		text   string
	}

	sorted := make([]editcore.Hunk, len(hunks))
	copy(sorted, hunks)
	sortHunksAscending(sorted)

	var out []outLine
	const contextLines = 3
	shown := make(map[int]bool)
	lastShown := 0

	for _, h := range sorted {
		ctxStart := h.Start - contextLines
		if ctxStart < 1 {
			ctxStart = 1
		}
		ctxBeforeEnd := h.Start
		if h.Kind == editcore.HunkInsert && h.Cursor == editcore.CursorAfter {
			ctxBeforeEnd = h.Start + 1
		}
		for i := ctxStart; i < ctxBeforeEnd; i++ {
			if shown[i] {
				continue
			}
			if lastShown > 0 && i > lastShown+1 {
				gap := i - lastShown - 1
				if gap >= 8 {
					out = append(out, outLine{text: fmt.Sprintf("… (%d unchanged lines)", gap)})
				}
			}
			newNo, ok := oldToNew[i]
			if ok && newNo > 0 {
				out = append(out, outLine{prefix: fmt.Sprintf(" %d:", newNo), text: oldLines[i-1]})
			} else {
				out = append(out, outLine{prefix: fmt.Sprintf(" %d:", i), text: oldLines[i-1]})
			}
			shown[i] = true
			lastShown = i
		}

		switch h.Kind {
		case editcore.HunkReplace:
			for l := h.Start; l <= h.End; l++ {
				out = append(out, outLine{prefix: fmt.Sprintf("-%d:", l), text: safeGet(oldLines, l-1)})
				shown[l] = true
				lastShown = l
			}
			insStart := h.Start
			if ns, ok := oldToNew[h.Start]; ok && ns > 0 {
				insStart = ns
			} else if h.Start > 1 {
				if ns, ok := oldToNew[h.Start-1]; ok && ns > 0 {
					insStart = ns + 1
				}
			}
			for k, line := range h.Payload {
				out = append(out, outLine{prefix: fmt.Sprintf("+%d:", insStart+k), text: line})
			}

		case editcore.HunkDelete:
			for l := h.Start; l <= h.End; l++ {
				out = append(out, outLine{prefix: fmt.Sprintf("-%d:", l), text: safeGet(oldLines, l-1)})
				shown[l] = true
				lastShown = l
			}

		case editcore.HunkInsert:
			insStart := computeInsertStart(h, oldToNew, len(newLines))
			for k, line := range h.Payload {
				out = append(out, outLine{prefix: fmt.Sprintf("+%d:", insStart+k), text: line})
			}
		}

		hunkEnd := h.End
		if h.Kind == editcore.HunkInsert {
			hunkEnd = h.Start
			if h.Cursor == editcore.CursorAfter {
				hunkEnd = h.Start + 1
			}
		}
		if hunkEnd < 1 {
			hunkEnd = 1
		}
		ctxEnd := hunkEnd + contextLines
		if ctxEnd > len(oldLines) {
			ctxEnd = len(oldLines)
		}
		for i := hunkEnd; i <= ctxEnd; i++ {
			if shown[i] {
				continue
			}
			if lastShown > 0 && i > lastShown+1 {
				gap := i - lastShown - 1
				if gap >= 8 {
					out = append(out, outLine{text: fmt.Sprintf("… (%d unchanged lines)", gap)})
				}
			}
			newNo, ok := oldToNew[i]
			if ok && newNo > 0 {
				out = append(out, outLine{prefix: fmt.Sprintf(" %d:", newNo), text: oldLines[i-1]})
			} else {
				out = append(out, outLine{prefix: fmt.Sprintf(" %d:", i), text: oldLines[i-1]})
			}
			shown[i] = true
			lastShown = i
		}
	}

	var sb strings.Builder
	for _, l := range out {
		if l.prefix == "" {
			fmt.Fprintf(&sb, " %s\n", l.text)
		} else {
			fmt.Fprintf(&sb, "%s%s\n", l.prefix, l.text)
		}
	}
	return strings.TrimRight(sb.String(), "\n")
}
