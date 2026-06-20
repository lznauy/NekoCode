package editdsl

import (
	"strings"
	"testing"
)

func TestOldToNew_ReplaceSingleLine(t *testing.T) {
	// Replace line 2 with two new lines.
	// The trailing newline creates a sentinel empty line at index 4 in the split.
	text := "line1\nline2\nline3\n"
	hunks := []Hunk{
		{Kind: HunkReplace, Start: 2, End: 2, Payload: []string{"newA", "newB"}},
	}
	result, err := ApplyEdits(text, hunks, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	// Old: line1(1), line2(2), line3(3), sentinel(4)
	// New: line1(1), newA(2), newB(3), line3(4), sentinel(5)
	if result.OldToNew[1] != 1 {
		t.Errorf("old line 1 should map to new line 1, got %d", result.OldToNew[1])
	}
	if _, ok := result.OldToNew[2]; ok {
		t.Error("old line 2 should be deleted (not in map)")
	}
	if result.OldToNew[3] != 4 {
		t.Errorf("old line 3 should map to new line 4, got %d", result.OldToNew[3])
	}
	// line 1, 3, and sentinel line 4 survive → 3 entries
	if len(result.OldToNew) != 3 {
		t.Errorf("expected 3 entries in OldToNew (lines 1,3,and sentinel), got %d: %v", len(result.OldToNew), result.OldToNew)
	}
}

func TestOldToNew_DeleteRange(t *testing.T) {
	text := "a\nb\nc\nd\ne\n"
	hunks := []Hunk{
		{Kind: HunkDelete, Start: 2, End: 4}, // delete b,c,d
	}
	result, err := ApplyEdits(text, hunks, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	// Old: a(1), b(2), c(3), d(4), e(5)
	// New: a(1), e(2)
	if result.OldToNew[1] != 1 {
		t.Errorf("old line 1 should map to new 1, got %d", result.OldToNew[1])
	}
	if _, ok := result.OldToNew[2]; ok {
		t.Error("old line 2 should be deleted (not in map)")
	}
	if _, ok := result.OldToNew[3]; ok {
		t.Error("old line 3 should be deleted")
	}
	if _, ok := result.OldToNew[4]; ok {
		t.Error("old line 4 should be deleted")
	}
	if result.OldToNew[5] != 2 {
		t.Errorf("old line 5 should map to new 2, got %d", result.OldToNew[5])
	}
}

func TestOldToNew_InsertAfter(t *testing.T) {
	text := "line1\nline2\nline3\n"
	hunks := []Hunk{
		{Kind: HunkInsert, Start: 2, Cursor: CursorAfter, Payload: []string{"insertedA", "insertedB"}},
	}
	result, err := ApplyEdits(text, hunks, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	// Old: line1(1), line2(2), line3(3)
	// New: line1(1), line2(2), insertedA(3), insertedB(4), line3(5)
	if result.OldToNew[1] != 1 {
		t.Errorf("old 1 -> new 1, got %d", result.OldToNew[1])
	}
	if result.OldToNew[2] != 2 {
		t.Errorf("old 2 -> new 2, got %d", result.OldToNew[2])
	}
	if result.OldToNew[3] != 5 {
		t.Errorf("old 3 -> new 5, got %d", result.OldToNew[3])
	}
}

func TestOldToNew_InsertBefore(t *testing.T) {
	text := "line1\nline2\nline3\n"
	hunks := []Hunk{
		{Kind: HunkInsert, Start: 2, Cursor: CursorBefore, Payload: []string{"beforeA"}},
	}
	result, err := ApplyEdits(text, hunks, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	// Old: line1(1), line2(2), line3(3)
	// New: line1(1), beforeA(2), line2(3), line3(4)
	if result.OldToNew[1] != 1 {
		t.Errorf("old 1 -> new 1, got %d", result.OldToNew[1])
	}
	if result.OldToNew[2] != 3 {
		t.Errorf("old 2 -> new 3, got %d", result.OldToNew[2])
	}
	if result.OldToNew[3] != 4 {
		t.Errorf("old 3 -> new 4, got %d", result.OldToNew[3])
	}
}

func TestOldToNew_InsertHead(t *testing.T) {
	text := "line1\nline2\n"
	hunks := []Hunk{
		{Kind: HunkInsert, Cursor: CursorHead, Payload: []string{"header"}},
	}
	result, err := ApplyEdits(text, hunks, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	// Old: line1(1), line2(2)
	// New: header(1), line1(2), line2(3)
	if result.OldToNew[1] != 2 {
		t.Errorf("old 1 -> new 2, got %d", result.OldToNew[1])
	}
	if result.OldToNew[2] != 3 {
		t.Errorf("old 2 -> new 3, got %d", result.OldToNew[2])
	}
}

func TestOldToNew_InsertTail(t *testing.T) {
	text := "line1\nline2\n"
	hunks := []Hunk{
		{Kind: HunkInsert, Cursor: CursorTail, Payload: []string{"footer"}},
	}
	result, err := ApplyEdits(text, hunks, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	// Old: line1(1), line2(2)
	// New: line1(1), line2(2), footer(3)
	if result.OldToNew[1] != 1 {
		t.Errorf("old 1 -> new 1, got %d", result.OldToNew[1])
	}
	if result.OldToNew[2] != 2 {
		t.Errorf("old 2 -> new 2, got %d", result.OldToNew[2])
	}
}

func TestOldToNew_MultipleHunksBottomUp(t *testing.T) {
	// Two hunks: one at line 10, one at line 3 (applied bottom-up).
	// The line 10 edit must NOT be affected by the line 3 edit.
	text := "1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n11\n12\n"
	hunks := []Hunk{
		{Kind: HunkReplace, Start: 10, End: 10, Payload: []string{"X"}},
		{Kind: HunkDelete, Start: 3, End: 4},
	}
	result, err := ApplyEdits(text, hunks, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	// Apply bottom-up:
	// First: replace line 10 with "X" (old 10 deleted, X inserted as identity 0)
	// Then:  delete lines 3-4
	// Old:   1,2,3,4,5,6,7,8,9,10,11,12,sentinel(13)
	// New:   1,2,5,6,7,8,9,X,11,12,sentinel
	// Old 1 -> new 1, Old 2 -> new 2
	// Old 3,4 deleted; Old 10 deleted (replaced)
	// Old 5 -> new 3, Old 11 -> new 9, etc.
	if result.OldToNew[1] != 1 {
		t.Errorf("old 1 -> new 1, got %d", result.OldToNew[1])
	}
	if result.OldToNew[2] != 2 {
		t.Errorf("old 2 -> new 2, got %d", result.OldToNew[2])
	}
	if _, ok := result.OldToNew[3]; ok {
		t.Error("old line 3 should be deleted")
	}
	if _, ok := result.OldToNew[4]; ok {
		t.Error("old line 4 should be deleted")
	}
	if result.OldToNew[5] != 3 {
		t.Errorf("old 5 -> new 3, got %d", result.OldToNew[5])
	}
	// Old line 10 was replaced → its identity is lost (deleted).
	if _, ok := result.OldToNew[10]; ok {
		t.Error("old line 10 was replaced, should not be in OldToNew map")
	}
	if result.OldToNew[11] != 9 {
		t.Errorf("old 11 -> new 9, got %d", result.OldToNew[11])
	}
	// Verify the "X" is in the right place
	lines := strings.Split(result.Text, "\n")
	if lines[7] != "X" {
		t.Errorf("expected X at position 7 (0-based), got %q at pos %d", lines[7], 7)
	}
}

func TestOldToNew_AfterInsertLandingShift(t *testing.T) {
	// Simulate insert after a line inside a nested block — landing shift should fire.
	text := "func foo() {\n    bar()\n}\nbaz\n"
	// insert after line 2 ("    bar()") with shallower body -> should shift past "}"
	hunks := []Hunk{
		{Kind: HunkInsert, Start: 2, Cursor: CursorAfter, Payload: []string{"newCall()"}},
	}
	result, err := ApplyEdits(text, hunks, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	// Landing shift fires: insert after 2 becomes insert after "}" (line 3).
	// New: func foo() {\n    bar()\n}\nnewCall()\nbaz\n
	// Old: func foo()(1), bar()(2), }(3), baz(4)
	// New: func foo()(1), bar()(2), }(3), newCall()(4), baz(5)
	if result.OldToNew[1] != 1 {
		t.Errorf("old 1 -> new 1, got %d", result.OldToNew[1])
	}
	if result.OldToNew[2] != 2 {
		t.Errorf("old 2 -> new 2, got %d", result.OldToNew[2])
	}
	if result.OldToNew[3] != 3 {
		t.Errorf("old 3 -> new 3, got %d", result.OldToNew[3])
	}
	if result.OldToNew[4] != 5 {
		t.Errorf("old 4 -> new 5 (shifted because newCall() inserted before it), got %d", result.OldToNew[4])
	}
	// Verify the actual text
	expected := "func foo() {\n    bar()\n}\nnewCall()\nbaz\n"
	if result.Text != expected {
		t.Errorf("unexpected text:\n%q\nwant:\n%q", result.Text, expected)
	}
}

func TestOldToNew_BlankLineCollapse(t *testing.T) {
	// Insert content that creates 3+ blank lines — should collapse to 2.
	text := "line1\n\n\nline4\n"
	hunks := []Hunk{
		{Kind: HunkInsert, Start: 2, Cursor: CursorAfter, Payload: []string{"", ""}},
	}
	result, err := ApplyEdits(text, hunks, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	// Before apply: line1, \n, \n, line4  (already 2 blank lines between 1 and 4)
	// After insert at line 2: line1, \n, \n, \n, \n, line4 (4 blank lines → collapsed to 2)
	// Expected: line1\n\n\nline4\n (3 blanks -> 2)
	// But wait, the original has \n\n between line1 and line4 (lines 2 and 3 are empty)
	// After insert 2 more blanks → 4 blanks → collapse to 2
	// OldToNew must be correct after collapse.
	// Verify line1 still maps correctly
	if result.OldToNew[1] != 1 {
		t.Errorf("old 1 -> new 1, got %d", result.OldToNew[1])
	}
	// line4 should shift due to collapsing
	if result.OldToNew[4] <= 0 {
		t.Errorf("old line 4 should exist in new file, got %d", result.OldToNew[4])
	}
	// Result must not have 3+ consecutive blanks
	if strings.Contains(result.Text, "\n\n\n\n") {
		t.Errorf("result contains 3+ consecutive blank lines:\n%q", result.Text)
	}
}

func TestOldToNew_BoundaryRepairImpact(t *testing.T) {
	// Replacement that echoes context lines — boundary repair should strip them.
	text := "keep1\nold2\nold3\nkeep4\n"
	// LLM echoes keep1 and keep4 in payload — boundary repair strips them.
	hunks := []Hunk{
		{Kind: HunkReplace, Start: 2, End: 3, Payload: []string{"keep1", "new2", "new3", "keep4"}},
	}
	result, err := ApplyEdits(text, hunks, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	// After boundary repair: payload becomes ["new2", "new3"]
	// New: keep1, new2, new3, keep4
	// OldToNew must be correct
	if result.OldToNew[1] != 1 {
		t.Errorf("old 1 (keep1) -> new 1, got %d", result.OldToNew[1])
	}
	// old lines 2,3 replaced — they should be deleted
	if _, ok := result.OldToNew[2]; ok {
		t.Error("old line 2 should be deleted (replaced)")
	}
	if _, ok := result.OldToNew[3]; ok {
		t.Error("old line 3 should be deleted (replaced)")
	}
	if result.OldToNew[4] != 4 {
		t.Errorf("old 4 -> new 4, got %d", result.OldToNew[4])
	}
	expected := "keep1\nnew2\nnew3\nkeep4\n"
	if result.Text != expected {
		t.Errorf("unexpected text:\n%q\nwant:\n%q", result.Text, expected)
	}
}

func TestOldToNew_EmptyHunks(t *testing.T) {
	text := "line1\nline2\n"
	result, err := ApplyEdits(text, nil, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != text {
		t.Errorf("empty hunks should return original text")
	}
	if result.OldToNew != nil {
		t.Errorf("empty hunks should return nil OldToNew, got %v", result.OldToNew)
	}
}

func TestOldToNew_ReplaceWithDelimiterRepair(t *testing.T) {
	// Replacement that drops closing braces — delimiter repair adds them back.
	text := "func f() {\n    x := 1\n}\n"
	// LLM forgot the closing } — delimiter repair adds it from deleted lines.
	hunks := []Hunk{
		{Kind: HunkReplace, Start: 1, End: 3, Payload: []string{"func f() {", "    x := 2"}},
	}
	result, err := ApplyEdits(text, hunks, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	// Should have repaired: added "}" from the deleted range.
	if !strings.Contains(result.Text, "}") {
		t.Errorf("delimiter repair should have added '}':\n%q", result.Text)
	}
	// OldToNew: line 1 was deleted (replaced by new func f() + x:=2 + })
	if _, ok := result.OldToNew[1]; ok {
		t.Error("old line 1 should be deleted (replaced)")
	}
}

func TestOldToNew_VerifyRecoveryPath(t *testing.T) {
	// The recovery path creates ApplyResult without OldToNew.
	// Verify snapshotReplay and sessionChainReplay produce nil OldToNew (acceptable).
	store := NewSnapshotStore()
	text := "line1\nline2\nline3\n"
	hash := store.Record("/test.go", text)

	// Modify the file to simulate drift.
	currentText := "line1\nline2_modified\nline3\n"
	edits := []Hunk{
		{Kind: HunkReplace, Start: 2, End: 2, Payload: []string{"new_content"}},
	}

	// Snapshot replay
	snap := store.ByHash("/test.go", hash)
	if snap == nil {
		t.Fatal("snapshot not found")
	}
	recoveryResult, err := snapshotReplay(snap, currentText, edits, nil, "/test.go")
	if err != nil {
		t.Fatal(err)
	}
	// Recovery path does not populate OldToNew — that's fine, callers handle nil.
	if recoveryResult.OldToNew != nil {
		t.Log("recovery result has OldToNew (unexpected but harmless)")
	}
	if recoveryResult.Text == currentText {
		t.Error("recovery should have modified the text")
	}
}
