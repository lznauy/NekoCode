package editdsl

import (
	"fmt"
	"strconv"
	"strings"
)

// ParsePatch parses a hashline DSL string into a Patch.
//
// *** Begin Patch and *** End Patch are optional — silently consumed.
// Bare body rows (missing + prefix) are auto-prefixed so LLMs that
// forget the sigil still succeed.
func ParsePatch(input string) (*Patch, error) {
	lines := strings.Split(NormalizeToLF(input), "\n")
	p := &Patch{}
	i := 0

	// Skip leading blank lines, comments, and optional *** Begin Patch.
	for i < len(lines) && (strings.TrimSpace(lines[i]) == "" || strings.HasPrefix(strings.TrimSpace(lines[i]), "#")) {
		i++
	}
	if i < len(lines) && strings.TrimSpace(lines[i]) == "*** Begin Patch" {
		i++
	}
	// Skip more blank lines after envelope.
	for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
		i++
	}
	// Catch unified-diff hunk header as the first content line (before any
	// [PATH#TAG] header) so the model sees a focused error, matching oh-my-pi.
	if i < len(lines) {
		firstTrimmed := strings.TrimSpace(lines[i])
		if unifiedDiffRe.MatchString(firstTrimmed) {
			return nil, fmt.Errorf("unified-diff hunk header (@@ ... @@) is not valid in editdsl. " +
				"File sections start with [PATH#TAG]; use replace, delete, or insert ops.")
		}
	}

	for i < len(lines) {
		line := strings.TrimSpace(lines[i])

		// End markers (both optional).
		if line == "*** Abort" {
			return nil, fmt.Errorf("patch aborted by author (*** Abort marker encountered)")
		}
		if line == "*** End Patch" {
			if len(p.Files) == 0 {
				return nil, fmt.Errorf("patch has no file sections")
			}
			return p, nil
		}

		// Skip blank lines.
		if line == "" {
			i++
			continue
		}

		// File header: [path#TAG]
		fp, newI, err := parseFilePatch(lines, i)
		if err != nil {
			return nil, err
		}
		p.Files = append(p.Files, *fp)
		i = newI
	}

	if len(p.Files) == 0 {
		return nil, fmt.Errorf("patch has no file sections")
	}
	// Merge consecutive sections targeting the same path so all hunks
	// anchor against the same snapshot and apply as one batch. Without
	// this, a follow-up section's anchors are stale after the first
	// edit shifts line numbers.
	var mergeErr error
	p.Files, mergeErr = mergeSamePathSections(p.Files)
	if mergeErr != nil {
		return nil, mergeErr
	}
	return p, nil
}

// mergeSamePathSections collapses FilePatches targeting the same path into
// a single patch with combined hunks. Conflicting tags are rejected.
func mergeSamePathSections(files []FilePatch) ([]FilePatch, error) {
	if len(files) <= 1 {
		return files, nil
	}
	byPath := make(map[string]*FilePatch)
	order := make([]string, 0)
	for i := range files {
		fp := &files[i]
		if existing, ok := byPath[fp.Path]; ok {
			if existing.FileTag != fp.FileTag {
				return nil, fmt.Errorf(
					"conflicting hashline snapshot tags for %s: #%s and #%s — re-read the file and retry with one current header",
					fp.Path, existing.FileTag, fp.FileTag)
			}
			existing.Hunks = append(existing.Hunks, fp.Hunks...)
		} else {
			byPath[fp.Path] = fp
			order = append(order, fp.Path)
		}
	}
	merged := make([]FilePatch, 0, len(order))
	for _, path := range order {
		merged = append(merged, *byPath[path])
	}
	return merged, nil
}

// isHeaderLine reports whether line looks like a file section header:
//
//	[path#TAG]  — canonical brackets
//	path#TAG    — bare (LLMs often forget the brackets)
func isHeaderLine(line string) bool {
	if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
		return true
	}
	// Bare header: path ending with #XXXXXXXX (8 hex chars).
	if idx := strings.LastIndex(line, "#"); idx >= 0 && len(line)-idx-1 == 8 {
		for _, c := range line[idx+1:] {
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
				return false
			}
		}
		return idx > 0 // path part must be non-empty
	}
	return false
}

// parseHeader extracts path and file tag from a header line in either format.
func parseHeader(line string) (path, tag string, err error) {
	if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
		inner := line[1 : len(line)-1]
		idx := strings.LastIndex(inner, "#")
		if idx < 0 {
			return "", "", fmt.Errorf("file header missing '#' separator: %q", line)
		}
		path = stripApplyPatchNoise(inner[:idx])
		tag = inner[idx+1:]
	} else {
		idx := strings.LastIndex(line, "#")
		path = stripApplyPatchNoise(line[:idx])
		tag = line[idx+1:]
	}
	if len(tag) != 8 {
		return "", "", fmt.Errorf("file tag must be 8 hex chars, got %q", tag)
	}
	return path, tag, nil
}

func parseFilePatch(lines []string, i int) (*FilePatch, int, error) {
	line := strings.TrimSpace(lines[i])

	if !isHeaderLine(line) {
		return nil, i, fmt.Errorf("expected file header '[path#TAG]', got %q", line)
	}
	path, tag, err := parseHeader(line)
	if err != nil {
		return nil, i, err
	}
	fp := &FilePatch{Path: path, FileTag: tag}
	i++

	for i < len(lines) {
		line := strings.TrimSpace(lines[i])

		if line == "*** Abort" || line == "*** End Patch" || isHeaderLine(line) {
			break
		}

		if line == "" {
			i++
			continue
		}

		hunk, newI, err := parseHunk(lines, i)
		if err != nil {
			return nil, i, err
		}
		fp.Hunks = append(fp.Hunks, *hunk)
		i = newI
	}

	if len(fp.Hunks) == 0 {
		return nil, i, fmt.Errorf("file %q has no hunks", fp.Path)
	}
	return fp, i, nil
}

func parseHunk(lines []string, i int) (*Hunk, int, error) {
	line := strings.TrimSpace(lines[i])

	switch {
	case strings.HasPrefix(line, "replace block "):
		return parseReplaceBlockHunk(line, lines, i)
	case strings.HasPrefix(line, "replace "):
		return parseReplaceHunk(line, lines, i)
	case strings.HasPrefix(line, "delete block "):
		return parseDeleteBlockHunk(line, lines, i)
	case strings.HasPrefix(line, "delete "):
		return parseDeleteHunk(line, lines, i)
	case strings.HasPrefix(line, "insert after block "):
		return parseInsertAfterBlockHunk(line, lines, i)
	case strings.HasPrefix(line, "insert "):
		return parseInsertHunk(line, lines, i)
	default:
		// Detect common LLM mistakes.
		if cont := detectApplyPatchContamination(line); cont != nil {
			return nil, i, fmt.Errorf("line %d: %s", i+1, *cont)
		}
		return nil, i, fmt.Errorf("unknown hunk type: %q", line)
	}
}

func parseReplaceBlockHunk(header string, lines []string, i int) (*Hunk, int, error) {
	spec := strings.TrimPrefix(header, "replace block ")
	spec = strings.TrimSuffix(spec, ":")
	spec = strings.TrimSpace(spec)

	n, err := strconv.Atoi(spec)
	if err != nil {
		return nil, i, fmt.Errorf("invalid block line number: %w", err)
	}
	i++

	payload, newI, err := parsePayload(lines, i)
	if err != nil {
		return nil, i, err
	}
	if len(payload) == 0 {
		return nil, i, fmt.Errorf("replace block hunk at line %d has no payload", n)
	}

	return &Hunk{
		Kind:    HunkReplace,
		Start:   n,
		End:     n,
		Block:   true,
		Payload: payload,
	}, newI, nil
}

func parseDeleteBlockHunk(header string, lines []string, i int) (*Hunk, int, error) {
	spec := strings.TrimPrefix(header, "delete block ")
	spec = strings.TrimSpace(spec)

	n, err := strconv.Atoi(spec)
	if err != nil {
		return nil, i, fmt.Errorf("invalid block line number: %w", err)
	}

	// Reject body rows on delete hunks.
	ni := i + 1
	if ni < len(lines) {
		next := strings.TrimSpace(lines[ni])
		if strings.HasPrefix(next, "+") {
			return nil, i, fmt.Errorf("delete block takes no body — remove the '+' rows after delete block %d", n)
		}
	}

	return &Hunk{
		Kind:  HunkDelete,
		Start: n,
		End:   n,
		Block: true,
	}, ni, nil
}

func parseInsertAfterBlockHunk(header string, lines []string, i int) (*Hunk, int, error) {
	spec := strings.TrimPrefix(header, "insert after block ")
	spec = strings.TrimSuffix(spec, ":")
	spec = strings.TrimSpace(spec)

	n, err := strconv.Atoi(spec)
	if err != nil {
		return nil, i, fmt.Errorf("invalid block line number: %w", err)
	}
	i++

	payload, newI, err := parsePayload(lines, i)
	if err != nil {
		return nil, i, err
	}
	if len(payload) == 0 {
		return nil, i, fmt.Errorf("insert after block hunk at line %d has no payload", n)
	}

	return &Hunk{
		Kind:    HunkInsert,
		Start:   n,
		Cursor:  CursorAfter,
		Block:   true,
		Payload: payload,
	}, newI, nil
}

func parseReplaceHunk(header string, lines []string, i int) (*Hunk, int, error) {
	spec := strings.TrimPrefix(header, "replace ")
	spec = strings.TrimSuffix(spec, ":")
	spec = strings.TrimSpace(spec)

	start, end, err := parseRange(spec)
	if err != nil {
		return nil, i, fmt.Errorf("invalid replace range %q: %w", spec, err)
	}
	i++

	payload, newI, err := parsePayload(lines, i)
	if err != nil {
		return nil, i, err
	}
	if len(payload) == 0 {
		return nil, i, fmt.Errorf("replace hunk at line %d has no payload", start)
	}

	return &Hunk{
		Kind:    HunkReplace,
		Start:   start,
		End:     end,
		Payload: payload,
	}, newI, nil
}

func parseDeleteHunk(header string, lines []string, i int) (*Hunk, int, error) {
	spec := strings.TrimPrefix(header, "delete ")
	spec = strings.TrimSuffix(spec, ":")
	spec = strings.TrimSpace(spec)

	start, end, err := parseRange(spec)
	if err != nil {
		return nil, i, fmt.Errorf("invalid delete range %q: %w", spec, err)
	}

	// Reject body rows on delete hunks (collected from parsePayload for replace/insert).
	ni := i + 1
	if ni < len(lines) {
		next := strings.TrimSpace(lines[ni])
		if strings.HasPrefix(next, "+") {
			return nil, i, fmt.Errorf("delete takes no body — remove the '+' rows after delete %d..%d", start, end)
		}
	}

	return &Hunk{
		Kind:  HunkDelete,
		Start: start,
		End:   end,
	}, ni, nil
}

func parseInsertHunk(header string, lines []string, i int) (*Hunk, int, error) {
	spec := strings.TrimPrefix(header, "insert ")
	spec = strings.TrimSuffix(spec, ":")
	spec = strings.TrimSpace(spec)

	var cursor CursorType
	var anchor int

	switch {
	case spec == "head":
		cursor = CursorHead
		anchor = 0
	case spec == "tail":
		cursor = CursorTail
		anchor = 0
	case strings.HasPrefix(spec, "head "):
		return nil, i, fmt.Errorf("insert %q: head accepts no line number; use \"insert head:\"", spec)
	case strings.HasPrefix(spec, "tail "):
		return nil, i, fmt.Errorf("insert %q: tail accepts no line number; use \"insert tail:\"", spec)
	case strings.HasPrefix(spec, "before "):
		cursor = CursorBefore
		n, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(spec, "before ")))
		if err != nil {
			return nil, i, fmt.Errorf("invalid insert anchor: %w", err)
		}
		anchor = n
	case strings.HasPrefix(spec, "after "):
		cursor = CursorAfter
		n, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(spec, "after ")))
		if err != nil {
			return nil, i, fmt.Errorf("invalid insert anchor: %w", err)
		}
		anchor = n
	default:
		return nil, i, fmt.Errorf("invalid insert position: %q", spec)
	}
	i++

	payload, newI, err := parsePayload(lines, i)
	if err != nil {
		return nil, i, err
	}
	if len(payload) == 0 {
		return nil, i, fmt.Errorf("insert hunk at %q has no payload", header)
	}

	return &Hunk{
		Kind:    HunkInsert,
		Start:   anchor,
		Cursor:  cursor,
		Payload: payload,
	}, newI, nil
}
