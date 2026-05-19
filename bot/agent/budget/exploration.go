package budget

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ExplorationTracker implements the decay-score mechanism:
// starts at 100, tools deduct, edits restore.
// When score <= 0, forced precipitation is triggered.
type ExplorationTracker struct {
	Score        int
	ReadFiles    map[string]int     // file path → read count (for re-read penalty)
	GenDeclEdits map[string]string  // file:genDeclName → last hash (for back-and-forth detection)
}

// NewExplorationTracker creates a fresh tracker with full score.
func NewExplorationTracker() *ExplorationTracker {
	return &ExplorationTracker{
		Score:        200,
		ReadFiles:    make(map[string]int),
		GenDeclEdits: make(map[string]string),
	}
}

// IsExhausted returns true when the exploration budget is depleted.
func (t *ExplorationTracker) IsExhausted() bool {
	return t.Score <= 0
}

// Reset fully restores the exploration budget, clearing read-file history.
func (t *ExplorationTracker) Reset() {
	t.Score = 200
	for k := range t.ReadFiles {
		delete(t.ReadFiles, k)
	}
}

// ---------------------------------------------------------------------------
// recording
// ---------------------------------------------------------------------------

// Record updates the score based on a tool call and its result.
// toolName: the tool that was called.
// filePath: for read/edit/write, the target file path.
// args: tool arguments (for detecting GenDecl vs trivial changes).
func (t *ExplorationTracker) Record(toolName string, filePath string) {
	switch toolName {
	case "list":
		t.deduct(2, "list")
	case "glob":
		t.deduct(2, "glob")
	case "grep":
		t.deduct(3, "grep")
	case "web_search":
		t.deduct(3, "web_search")
	case "web_fetch":
		t.deduct(8, "web_fetch")
	case "task":
		t.deduct(12, "task(explore)")
	case "read":
		t.recordRead(filePath)
	case "edit", "write":
		t.recordEdit(filePath)
	}
}

func (t *ExplorationTracker) recordRead(filePath string) {
	count := t.ReadFiles[filePath] + 1
	t.ReadFiles[filePath] = count
	switch count {
	case 1:
		t.deduct(5, "read:new:"+filepath.Base(filePath))
	case 2:
		t.deduct(10, "read:re-read:"+filepath.Base(filePath))
	default:
		penalty := 15 + (count-3)*5 // escalating
		t.deduct(penalty, fmt.Sprintf("read:re-read×%d:%s", count, filepath.Base(filePath)))
	}
}

func (t *ExplorationTracker) recordEdit(filePath string) {
	// Effective edit detection — evaluate actual impact.
	if t.isEffectiveEdit(filePath) {
		t.Reset()
	} else {
		t.deduct(5, "edit:trivial")
	}
}

// ---------------------------------------------------------------------------
// effective edit detection
// ---------------------------------------------------------------------------

// isEffectiveEdit detects score-farming edits (comment-only, whitespace-only, back-and-forth).
func (t *ExplorationTracker) isEffectiveEdit(filePath string) bool {
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return true // new files are always real work
	}
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".go":
		return t.checkGoEdit(filePath)
	case ".mod", ".sum":
		return true // dependency changes always matter
	case ".yaml", ".json", ".toml":
		return true // config changes always matter
	case ".md":
		return true // doc changes matter
	default:
		return true // unknown types: trust the model
	}
}

// checkGoEdit detects trivial Go edits using a declaration fingerprint.
// If the file's exported declarations are unchanged after edit → fake edit.
func (t *ExplorationTracker) checkGoEdit(filePath string) bool {
	fp := goDeclFingerprint(filePath)
	if fp == "" {
		return true // can't read file, give benefit of doubt
	}
	key := filePath + ":decl"
	if prev, ok := t.GenDeclEdits[key]; ok {
		if prev == fp {
			return false // same declarations as last edit → back-and-forth fraud
		}
	}
	t.GenDeclEdits[key] = fp
	return true
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// RecordPenalty adds a direct penalty (e.g., Quota Request cost).
func (t *ExplorationTracker) RecordPenalty(amount int, reason string) {
	t.deduct(amount, reason)
}

// goDeclFingerprint returns a hash-like string of exported declarations in a .go file.
// Used to detect back-and-forth edits (A→B→A) on GenDecl nodes.
func goDeclFingerprint(filePath string) string {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return ""
	}
	// Extract all lines starting with "func ", "type " (exported only: uppercase first char).
	var sigs []string
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "func ") || strings.HasPrefix(trimmed, "type ") {
			// Only exported (starts with uppercase after keyword).
			parts := strings.Fields(trimmed)
			if len(parts) >= 2 && len(parts[1]) > 0 && parts[1][0] >= 'A' && parts[1][0] <= 'Z' {
				sigs = append(sigs, trimmed)
			}
		}
	}
	return strings.Join(sigs, "\n")
}

func (t *ExplorationTracker) deduct(amount int, _ string) {
	t.Score -= amount
	if t.Score < 0 {
		t.Score = 0
	}
}
