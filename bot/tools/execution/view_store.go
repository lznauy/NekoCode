package execution

import (
	"fmt"
	"strings"
	"sync"

	"nekocode/bot/tools/editcore"
)

const maxViewEntries = 500

// FileView is the edit-aware view registered by read. It gives edit a
// deterministic way to verify that an intent targets text the agent has seen.
type FileView struct {
	Path       string
	Revision   string
	WindowID   string
	StartLine  int
	EndLine    int
	WindowHash string
	TotalLines int
}

type viewEntry struct {
	FileView
	LineHashes map[int]string
	Lines      map[int]string
}

// ResolvedRange is the current-file location for an edit target originally
// anchored in a read VIEW.
type ResolvedRange struct {
	StartLine int
	EndLine   int
	Relocated bool
}

// ViewStore stores read windows for later edit validation.
type ViewStore struct {
	mu      sync.RWMutex
	entries map[string]viewEntry
	order   []string
}

func NewViewStore() *ViewStore {
	return &ViewStore{entries: make(map[string]viewEntry)}
}

func (s *ViewStore) Register(path string, lines []string, startLine, endLine int) FileView {
	if s == nil {
		return FileView{}
	}
	total := len(lines)
	if startLine < 1 {
		startLine = 1
	}
	if endLine > total {
		endLine = total
	}
	if endLine < startLine {
		endLine = startLine
	}

	fullText := strings.Join(lines, "\n")
	revision := editcore.ComputeFileHash(fullText)
	windowLines := append([]string(nil), lines[startLine-1:endLine]...)
	windowHash := editcore.ComputeFileHash(strings.Join(windowLines, "\n"))
	windowID := fmt.Sprintf("W%d_%d_%s", startLine, endLine, windowHash[:min(6, len(windowHash))])

	hashes := make(map[int]string, len(windowLines))
	lineText := make(map[int]string, len(windowLines))
	for i, line := range windowLines {
		lineNo := startLine + i
		hashes[lineNo] = editcore.ComputeFileHash(line)
		lineText[lineNo] = line
	}

	view := FileView{
		Path:       normalizePath(path),
		Revision:   revision,
		WindowID:   windowID,
		StartLine:  startLine,
		EndLine:    endLine,
		WindowHash: windowHash,
		TotalLines: total,
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.entries[windowID]; !exists {
		s.order = append(s.order, windowID)
	}
	s.entries[windowID] = viewEntry{FileView: view, LineHashes: hashes, Lines: lineText}
	s.evictIfNeeded()
	return view
}

func (s *ViewStore) Get(windowID string) (FileView, bool) {
	if s == nil {
		return FileView{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.entries[windowID]
	return e.FileView, ok
}

func (s *ViewStore) ResolveRange(windowID, path, revision string, startLine, endLine int, lines []string) (ResolvedRange, error) {
	if s == nil {
		return ResolvedRange{}, fmt.Errorf("view store unavailable; re-read the file before editing")
	}
	s.mu.RLock()
	e, ok := s.entries[windowID]
	s.mu.RUnlock()
	if !ok {
		return ResolvedRange{}, fmt.Errorf("unknown window_id %q; re-read the target range", windowID)
	}
	safePath := normalizePath(path)
	if e.Path != safePath {
		return ResolvedRange{}, fmt.Errorf("window_id %q belongs to %s, not %s", windowID, e.Path, safePath)
	}
	if revision != "" && e.Revision != revision {
		return ResolvedRange{}, fmt.Errorf("base_revision %s does not match window revision %s", revision, e.Revision)
	}
	if startLine < e.StartLine || endLine > e.EndLine || endLine < startLine {
		return ResolvedRange{}, fmt.Errorf("range %d..%d is outside window %s lines %d..%d", startLine, endLine, windowID, e.StartLine, e.EndLine)
	}
	if startLine >= 1 && endLine <= len(lines) && s.rangeMatches(e, startLine, endLine, lines) {
		return ResolvedRange{StartLine: startLine, EndLine: endLine}, nil
	}

	newStart, ok := findUniqueSpan(e.targetLines(startLine, endLine), lines)
	if !ok {
		return ResolvedRange{}, fmt.Errorf("target lines %d..%d changed or are no longer unique; re-read before editing", startLine, endLine)
	}
	return ResolvedRange{
		StartLine: newStart,
		EndLine:   newStart + (endLine - startLine),
		Relocated: true,
	}, nil
}

func (s *ViewStore) rangeMatches(e viewEntry, startLine, endLine int, lines []string) bool {
	for lineNo := startLine; lineNo <= endLine; lineNo++ {
		expected := e.LineHashes[lineNo]
		actual := editcore.ComputeFileHash(lines[lineNo-1])
		if expected != actual {
			return false
		}
	}
	return true
}

func (e viewEntry) targetLines(startLine, endLine int) []string {
	out := make([]string, 0, endLine-startLine+1)
	for lineNo := startLine; lineNo <= endLine; lineNo++ {
		out = append(out, e.Lines[lineNo])
	}
	return out
}

func findUniqueSpan(target, lines []string) (int, bool) {
	if len(target) == 0 || len(target) > len(lines) {
		return 0, false
	}
	found := 0
	foundStart := 0
	for i := 0; i <= len(lines)-len(target); i++ {
		if spanEqual(lines[i:i+len(target)], target) {
			found++
			foundStart = i + 1
			if found > 1 {
				return 0, false
			}
		}
	}
	return foundStart, found == 1
}

func spanEqual(a, b []string) bool {
	for i := range a {
		if editcore.ComputeFileHash(a[i]) != editcore.ComputeFileHash(b[i]) {
			return false
		}
	}
	return true
}

func (s *ViewStore) evictIfNeeded() {
	for len(s.entries) > maxViewEntries && len(s.order) > 0 {
		delete(s.entries, s.order[0])
		s.order = s.order[1:]
	}
}
