package hashline

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Patch represents a complete hashline patch.
type Patch struct {
	Files []FilePatch
}

// FilePatch represents edits to a single file.
type FilePatch struct {
	Path    string // file path
	FileTag string // 8-char hex content tag from read
	Hunks   []Hunk
}

// HunkKind enumerates the supported edit operations.
type HunkKind int

const (
	HunkReplace HunkKind = iota
	HunkDelete
	HunkInsert
)

// CursorType specifies where an insert hunk lands relative to its anchor.
type CursorType int

const (
	CursorBefore CursorType = iota
	CursorAfter
	CursorHead
	CursorTail
)

// Hunk represents a single edit operation within a file.
type Hunk struct {
	Kind    HunkKind
	Start   int        // 1-based line number (or anchor for insert)
	End     int        // inclusive end line (for ranges); equals Start for single-line
	Cursor  CursorType // only for HunkInsert
	Block   bool       // true for "replace block N", "delete block N", "insert after block N"
	Payload []string   // +TEXT lines (for replace and insert)
}

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
			return nil, fmt.Errorf("unified-diff hunk header (@@ ... @@) is not valid in hashline. " +
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
//   [path#TAG]  — canonical brackets
//   path#TAG    — bare (LLMs often forget the brackets)
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

// detectApplyPatchContamination checks for common LLM mistakes:
// unified-diff headers, apply_patch tool confusion, bare line numbers, - rows.
func detectApplyPatchContamination(line string) *string {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return nil
	}
	// apply_patch-style sentinel (including bare *** prefix).
	if strings.HasPrefix(trimmed, "***") {
		msg := fmt.Sprintf("apply_patch format is not valid in hashline. Use [PATH#TAG] for file sections, not %q.", trimmed)
		return &msg
	}
	// Unified-diff hunk header.
	if strings.HasPrefix(trimmed, "@@") {
		msg := "unified-diff hunk header (@@ ... @@) is not valid in hashline. Use 'replace N..M:', 'delete N..M', or 'insert after N:'."
		return &msg
	}
	// Bare line number (no verb).
	if isAllDigits(trimmed) {
		msg := fmt.Sprintf("hunk headers need a verb. Use 'replace %s..%s:' to replace, or 'delete %s' to delete.", trimmed, trimmed, trimmed)
		return &msg
	}
	// Bare range without verb (e.g. "5..10" or "5..10:").
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

// parsePayload reads +TEXT lines until the next hunk header, file header, or end marker.
// Bare body rows (missing + prefix) are auto-prefixed with a sentinel so the caller
// can emit a warning. Lines starting with - are rejected.
func parsePayload(lines []string, i int) ([]string, int, error) {
	var literal []payloadRow  // +TEXT rows
	var deferred []payloadRow // blank rows awaiting a non-blank confirmation

	commitBlanks := func() {
		literal = append(literal, deferred...)
		deferred = nil
	}

	for i < len(lines) {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		// Stop at structural markers.
		if trimmed == "*** Abort" || trimmed == "*** End Patch" ||
			isHeaderLine(trimmed) ||
			strings.HasPrefix(trimmed, "replace ") ||
			strings.HasPrefix(trimmed, "delete ") ||
			strings.HasPrefix(trimmed, "insert ") {
			break
		}

		// Payload lines with + prefix: strip it.
		if strings.HasPrefix(trimmed, "+") {
			commitBlanks()
			literal = append(literal, payloadRow{text: trimmed[1:], bare: false})
			i++
			continue
		}

		// Blank rows after body content started are deferred.
		if trimmed == "" {
			if len(literal) > 0 || len(deferred) > 0 {
				deferred = append(deferred, payloadRow{text: "", bare: true})
			}
			i++
			continue
		}

		// Check for common LLM mistakes.
		if cont := detectApplyPatchContamination(trimmed); cont != nil {
			return nil, i, fmt.Errorf("line %d: %s", i+1, *cont)
		}

		// Lines starting with - are not valid (unified-diff deletion row).
		if strings.HasPrefix(trimmed, "-") {
			return nil, i, fmt.Errorf("line %d: '-' rows are not valid. "+
				"The range already names the lines being changed. "+
				"For a literal '-' line, write '+-...'.", i+1)
		}

		// Bare row: auto-prefix with sentinel.
		// Use the original line (right-trimmed only) to preserve
		// leading indentation. trimmed (TrimSpace) strips it.
		commitBlanks()
		literal = append(literal, payloadRow{text: strings.TrimRight(line, " \t\r"), bare: true})
		i++
	}

	// Strip read-output N: prefixes from bare rows when they uniformly carry them.
	stripBarePrefixesIfUniform(literal)

	// Build final payload, dropping trailing deferred blanks (layout separators).
	all := append(literal, deferred...)
	n := len(all)
	for n > 0 && all[n-1].bare && all[n-1].text == "" {
		n--
	}
	payload := make([]string, 0, n)
	for j := 0; j < n; j++ {
		if all[j].bare {
			payload = append(payload, autoprefixSentinel+all[j].text)
		} else {
			payload = append(payload, all[j].text)
		}
	}
	return payload, i, nil
}

// parseRange parses "N" or "N..M" into a 1-based inclusive range.
func parseRange(spec string) (start, end int, err error) {
	if idx := strings.Index(spec, ".."); idx >= 0 {
		start, err = strconv.Atoi(strings.TrimSpace(spec[:idx]))
		if err != nil {
			return 0, 0, fmt.Errorf("invalid start line: %w", err)
		}
		end, err = strconv.Atoi(strings.TrimSpace(spec[idx+2:]))
		if err != nil {
			return 0, 0, fmt.Errorf("invalid end line: %w", err)
		}
	} else {
		start, err = strconv.Atoi(strings.TrimSpace(spec))
		if err != nil {
			return 0, 0, fmt.Errorf("invalid line number: %w", err)
		}
		end = start
	}
	if start < 1 {
		return 0, 0, fmt.Errorf("line numbers must be >= 1, got %d", start)
	}
	if end < start {
		return 0, 0, fmt.Errorf("end line %d < start line %d", end, start)
	}
	return start, end, nil
}

// applyPatchPathNoiseRe matches apply_patch-style path noise prefixes.
// Handles: "*** Update File:foo.go" → "foo.go", "Update/File:bar.ts" → "bar.ts",
// "Update<File:baz", "Update(File):qux", "***foo.ts", etc.

// unifiedDiffRe matches unified-diff hunk headers like "@@ -1,5 +1,6 @@".
var unifiedDiffRe = regexp.MustCompile(`^@@\s+[-+]?\d+,?\d*\s+[-+]?\d+,?\d*\s+@@`)
var applyPatchPathNoiseRe = regexp.MustCompile(`^\*{0,3}\s*(?:(?:[Uu]pdate|[Aa]dd|[Dd]elete|[Mm]ove)[^A-Za-z0-9]*(?:[Ff]ile|[Tt]o)?[^A-Za-z0-9]*:)?\s*\*{0,3}\s*`)

// stripApplyPatchNoise removes apply_patch-style path noise from file headers.
func stripApplyPatchNoise(path string) string {
	return strings.TrimSpace(applyPatchPathNoiseRe.ReplaceAllString(path, ""))
}

// hlPrefixRe matches the read-output line-number prefix (e.g. "42:", ">>>42:", "+42:").
var hlPrefixRe = regexp.MustCompile(`^\s*(?:>>>|>>)?\s*(?:[+*-]\s*)?\d+:`)

// stripOneLeadingHashlinePrefix strips at most one leading hashline
// line-number prefix from a single line. Used on bare body rows that may
// have been pasted from read/search output.
func stripOneLeadingHashlinePrefix(line string) string {
	return hlPrefixRe.ReplaceAllString(line, "")
}

// bareLiteralValueRe matches lone quoted/string literals or numeric values,
// optionally comma-terminated — the shape of keyed dict/YAML body rows, not read-output paste.
var bareLiteralValueRe = regexp.MustCompile(`^\s*(?:"[^"]*"|'[^']*'|[-+]?\d+(?:\.\d+)?)\s*,?\s*$`)

type payloadRow = struct {
	text string
	bare bool
}

// stripBarePrefixesIfUniform strips a single read-output line-number prefix
// from every bare body row, but only when ALL bare rows carry one uniformly.
// A uniform set of prefixes signals content pasted from read/search output;
// a mixed set means the "N:" is genuine payload and stays. Rows with explicit
// "+" are never bare and are never touched.
func stripBarePrefixesIfUniform(rows []payloadRow) {
	r := rows
	var bareIdxs []int
	for i := range r {
		if r[i].bare && r[i].text != "" {
			bareIdxs = append(bareIdxs, i)
		}
	}
	if len(bareIdxs) < 1 {
		return
	}
	// Check uniformity: every bare row must carry a prefix.
	for _, i := range bareIdxs {
		stripped := stripOneLeadingHashlinePrefix(r[i].text)
		if stripped == r[i].text {
			return // not all carry a prefix
		}
	}
	// If every stripped remainder is a lone quoted/numeric literal,
	// this is dict/YAML body, not read-output paste. Leave untouched.
	allLiterals := true
	for _, i := range bareIdxs {
		stripped := stripOneLeadingHashlinePrefix(r[i].text)
		if !bareLiteralValueRe.MatchString(stripped) {
			allLiterals = false
			break
		}
	}
	if allLiterals {
		return
	}
	// Strip prefix from all bare rows.
	for _, i := range bareIdxs {
		r[i].text = stripOneLeadingHashlinePrefix(r[i].text)
	}
}

