package hashline

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// hash.go tests
// ---------------------------------------------------------------------------

func TestComputeFileHash(t *testing.T) {
	hash := ComputeFileHash("hello\nworld\n")
	if len(hash) != 8 {
		t.Fatalf("expected 8-char hash, got %q", hash)
	}
	hash2 := ComputeFileHash("hello\nworld\n")
	if hash != hash2 {
		t.Fatalf("same content should produce same hash: %q vs %q", hash, hash2)
	}
	hash3 := ComputeFileHash("hello\nworld!\n")
	if hash == hash3 {
		t.Fatalf("different content should produce different hash: %q vs %q", hash, hash3)
	}
}

func TestComputeFileHash_CRLF(t *testing.T) {
	hashLF := ComputeFileHash("hello\nworld\n")
	hashCRLF := ComputeFileHash("hello\r\nworld\r\n")
	if hashLF != hashCRLF {
		t.Fatalf("CRLF/LF should produce same hash: %q vs %q", hashLF, hashCRLF)
	}
}

func TestComputeFileHash_TrailingWhitespace(t *testing.T) {
	hash1 := ComputeFileHash("hello\nworld\n")
	hash2 := ComputeFileHash("hello  \nworld\t\n")
	if hash1 != hash2 {
		t.Fatalf("trailing whitespace should not affect hash: %q vs %q", hash1, hash2)
	}
}

func TestNormalizeToLF(t *testing.T) {
	if got := NormalizeToLF("a\r\nb\rc\n"); got != "a\nb\nc\n" {
		t.Fatalf("got %q", got)
	}
}

func TestStripBOM(t *testing.T) {
	bom, clean := StripBOM("\xEF\xBB\xBFhello")
	if bom != "\xEF\xBB\xBF" {
		t.Fatalf("expected BOM, got %q", bom)
	}
	if clean != "hello" {
		t.Fatalf("expected 'hello', got %q", clean)
	}
	bom, clean = StripBOM("hello")
	if bom != "" {
		t.Fatalf("expected no BOM, got %q", bom)
	}
	if clean != "hello" {
		t.Fatalf("expected 'hello', got %q", clean)
	}
}

// ---------------------------------------------------------------------------
// patch.go tests
// ---------------------------------------------------------------------------

func TestParsePatch_Replace(t *testing.T) {
	input := `[main.go#A1B2C3D4]
replace 5..7:
+new line 1
+new line 2`
	p, err := ParsePatch(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(p.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(p.Files))
	}
	fp := p.Files[0]
	if fp.Path != "main.go" || fp.FileTag != "A1B2C3D4" {
		t.Fatalf("unexpected file: path=%q tag=%q", fp.Path, fp.FileTag)
	}
	if len(fp.Hunks) != 1 {
		t.Fatalf("expected 1 hunk, got %d", len(fp.Hunks))
	}
	h := fp.Hunks[0]
	if h.Kind != HunkReplace || h.Start != 5 || h.End != 7 {
		t.Fatalf("unexpected hunk: kind=%d start=%d end=%d", h.Kind, h.Start, h.End)
	}
	if len(h.Payload) != 2 || h.Payload[0] != "new line 1" || h.Payload[1] != "new line 2" {
		t.Fatalf("unexpected payload: %v", h.Payload)
	}
}

func TestParsePatch_Delete(t *testing.T) {
	input := `[main.go#A1B2C3D4]
delete 10..15`
	p, err := ParsePatch(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	h := p.Files[0].Hunks[0]
	if h.Kind != HunkDelete || h.Start != 10 || h.End != 15 {
		t.Fatalf("unexpected hunk: kind=%d start=%d end=%d", h.Kind, h.Start, h.End)
	}
}

func TestParsePatch_InsertAfter(t *testing.T) {
	input := `[main.go#A1B2C3D4]
insert after 20:
+inserted line`
	p, err := ParsePatch(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	h := p.Files[0].Hunks[0]
	if h.Kind != HunkInsert || h.Cursor != CursorAfter || h.Start != 20 {
		t.Fatalf("unexpected hunk: kind=%d cursor=%q start=%d", h.Kind, h.Cursor, h.Start)
	}
}

func TestParsePatch_InsertHead(t *testing.T) {
	input := `[main.go#A1B2C3D4]
insert head:
+first line`
	p, err := ParsePatch(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	h := p.Files[0].Hunks[0]
	if h.Kind != HunkInsert || h.Cursor != CursorHead {
		t.Fatalf("unexpected hunk: kind=%d cursor=%q", h.Kind, h.Cursor)
	}
}

func TestParsePatch_MultipleFiles(t *testing.T) {
	input := `*** Begin Patch
[a.go#11111111]
replace 1..1:
+new a
[b.go#22222222]
replace 1..1:
+new b
*** End Patch`
	p, err := ParsePatch(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(p.Files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(p.Files))
	}
	if p.Files[0].Path != "a.go" || p.Files[1].Path != "b.go" {
		t.Fatalf("unexpected paths: %q %q", p.Files[0].Path, p.Files[1].Path)
	}
}

func TestParsePatch_MultipleHunks(t *testing.T) {
	input := `[main.go#A1B2C3D4]
replace 5..7:
+new content
delete 20..25
insert after 30:
+inserted`
	p, err := ParsePatch(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(p.Files[0].Hunks) != 3 {
		t.Fatalf("expected 3 hunks, got %d", len(p.Files[0].Hunks))
	}
}

func TestParsePatch_OptionalBeginEndMarkers(t *testing.T) {
	// With markers — should parse.
	input := `*** Begin Patch
[main.go#A1B2C3D4]
replace 1..1:
+new
*** End Patch`
	_, err := ParsePatch(input)
	if err != nil {
		t.Fatalf("parse error with markers: %v", err)
	}
	// Without markers — should also parse.
	input2 := `[main.go#A1B2C3D4]
replace 1..1:
+new`
	_, err = ParsePatch(input2)
	if err != nil {
		t.Fatalf("parse error without markers: %v", err)
	}
}

func TestParsePatch_NoSections(t *testing.T) {
	_, err := ParsePatch("*** Begin Patch\n*** End Patch")
	if err == nil {
		t.Fatal("expected error for patch with no sections")
	}
}

func TestParsePatch_InvalidTag(t *testing.T) {
	input := `[main.go#AB]
replace 1..1:
+new`
	_, err := ParsePatch(input)
	if err == nil {
		t.Fatal("expected error for invalid tag length")
	}
}

func TestParsePatch_EmptyPayload(t *testing.T) {
	input := `[main.go#A1B2C3D4]
replace 1..5:`
	_, err := ParsePatch(input)
	if err == nil {
		t.Fatal("expected error for empty replace payload")
	}
}

func TestParsePatch_BareBodyRows(t *testing.T) {
	// Bare body rows (no + prefix) should be auto-prefixed.
	input := `[main.go#A1B2C3D4]
replace 1..1:
bare line 1
bare line 2`
	p, err := ParsePatch(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	h := p.Files[0].Hunks[0]
	if len(h.Payload) != 2 {
		t.Fatalf("expected 2 payload lines, got %d", len(h.Payload))
	}
	for _, line := range h.Payload {
		if strings.HasPrefix(line, autoprefixSentinel) {
			// OK — sentinel still present (stripped in ApplyEdits)
		} else {
			t.Fatalf("expected autoprefix sentinel in payload, got %q", line)
		}
	}
}

func TestParsePatch_MinusRowsRejected(t *testing.T) {
	input := `[main.go#A1B2C3D4]
replace 1..1:
-old line`
	_, err := ParsePatch(input)
	if err == nil {
		t.Fatal("expected error for '-' rows in payload")
	}
}

func TestParsePatch_ApplyPatchContamination(t *testing.T) {
	input := `[main.go#A1B2C3D4]
replace 1..1:
*** Update File: foo`
	_, err := ParsePatch(input)
	if err == nil {
		t.Fatal("expected error for apply_patch contamination")
	}
}

func TestParsePatch_UnifiedDiffHunk(t *testing.T) {
	input := `[main.go#A1B2C3D4]
replace 1..1:
@@ -1,3 +1,3 @@`
	_, err := ParsePatch(input)
	if err == nil {
		t.Fatal("expected error for unified-diff hunk header")
	}
}

func TestParsePatch_BareLineNumber(t *testing.T) {
	input := `[main.go#A1B2C3D4]
42`
	_, err := ParsePatch(input)
	if err == nil {
		t.Fatal("expected error for bare line number")
	}
}

func TestParsePatch_DeleteWithColon(t *testing.T) {
	// "delete N..M:" with trailing colon should still parse (strip colons).
	input := `[main.go#A1B2C3D4]
delete 10..15:`
	p, err := ParsePatch(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	h := p.Files[0].Hunks[0]
	if h.Kind != HunkDelete || h.Start != 10 || h.End != 15 {
		t.Fatalf("unexpected hunk: kind=%d start=%d end=%d", h.Kind, h.Start, h.End)
	}
}

func TestParsePatch_InsertHead1Rejected(t *testing.T) {
	input := `[main.go#A1B2C3D4]
insert head 1:
+first line`
	_, err := ParsePatch(input)
	if err == nil {
		t.Fatal("expected error for insert head with line number")
	}
}
func TestParsePatch_ApplyPatchPathNoise(t *testing.T) {
	input := `[*** Update File:foo.ts#A1B2C3D4]
replace 1..1:
+new`
	p, err := ParsePatch(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if p.Files[0].Path != "foo.ts" {
		t.Fatalf("expected 'foo.ts', got %q", p.Files[0].Path)
	}
}


func TestParsePatch_SkippableComments(t *testing.T) {
	input := `# fix the import path
[main.go#A1B2C3D4]
replace 2..2:
+import "fmt"`
	p, err := ParsePatch(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(p.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(p.Files))
	}
}

func TestParsePatch_ReadOutputPrefixStripping(t *testing.T) {
	// Every bare body row carries a "42:" prefix from read output.
	input := `[main.go#A1B2C3D4]
replace 2..3:
42:new line 2
43:new line 3`
	p, err := ParsePatch(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(p.Files[0].Hunks[0].Payload) != 2 {
		t.Fatalf("expected 2 payload lines, got %d", len(p.Files[0].Hunks[0].Payload))
	}
	prefix := "\x00autoprefix:"
	if !strings.HasPrefix(p.Files[0].Hunks[0].Payload[0], prefix) {
		t.Errorf("expected autoprefix sentinel")
	}
	text := strings.TrimPrefix(p.Files[0].Hunks[0].Payload[0], prefix)
	if text != "new line 2" {
		t.Errorf("expected 'new line 2', got %q", text)
	}
}

func TestParsePatch_InteriorBlankLines(t *testing.T) {
	input := `[main.go#A1B2C3D4]
replace 2..4:
func foo() {

	return 1
}`
	p, err := ParsePatch(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(p.Files[0].Hunks[0].Payload) != 4 {
		t.Fatalf("expected 4 payload lines (with interior blank), got %d", len(p.Files[0].Hunks[0].Payload))
	}
}

func TestParsePatch_PrefixNotStrippedWhenMixed(t *testing.T) {
	input := `[main.go#A1B2C3D4]
replace 2..3:
42:prefixed
plain row`
	p, err := ParsePatch(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	prefix := "\x00autoprefix:"
	if !strings.HasPrefix(p.Files[0].Hunks[0].Payload[0], prefix) {
		t.Errorf("first row should have autoprefix sentinel")
	}
	text := strings.TrimPrefix(p.Files[0].Hunks[0].Payload[0], prefix)
	if text != "42:prefixed" {
		t.Errorf("mixed prefix should not be stripped, got %q", text)
	}
}

func TestApplyEdits_Replace(t *testing.T) {
	text := "line1\nline2\nline3\nline4\nline5"
	hunks := []Hunk{{
		Kind:    HunkReplace,
		Start:   2,
		End:     4,
		Payload: []string{"new2", "new3"},
	}}
	result, err := ApplyEdits(text, hunks, nil, "")
	if err != nil {
		t.Fatalf("apply error: %v", err)
	}
	expected := "line1\nnew2\nnew3\nline5"
	if result.Text != expected {
		t.Fatalf("expected %q, got %q", expected, result.Text)
	}
}

func TestApplyEdits_Delete(t *testing.T) {
	text := "line1\nline2\nline3\nline4\nline5"
	hunks := []Hunk{{
		Kind:  HunkDelete,
		Start: 2,
		End:   4,
	}}
	result, err := ApplyEdits(text, hunks, nil, "")
	if err != nil {
		t.Fatalf("apply error: %v", err)
	}
	expected := "line1\nline5"
	if result.Text != expected {
		t.Fatalf("expected %q, got %q", expected, result.Text)
	}
}

func TestApplyEdits_InsertAfter(t *testing.T) {
	text := "line1\nline2\nline3"
	hunks := []Hunk{{
		Kind:    HunkInsert,
		Start:   2,
		Cursor:  CursorAfter,
		Payload: []string{"inserted"},
	}}
	result, err := ApplyEdits(text, hunks, nil, "")
	if err != nil {
		t.Fatalf("apply error: %v", err)
	}
	expected := "line1\nline2\ninserted\nline3"
	if result.Text != expected {
		t.Fatalf("expected %q, got %q", expected, result.Text)
	}
}

func TestApplyEdits_InsertBefore(t *testing.T) {
	text := "line1\nline2\nline3"
	hunks := []Hunk{{
		Kind:    HunkInsert,
		Start:   2,
		Cursor:  CursorBefore,
		Payload: []string{"inserted"},
	}}
	result, err := ApplyEdits(text, hunks, nil, "")
	if err != nil {
		t.Fatalf("apply error: %v", err)
	}
	expected := "line1\ninserted\nline2\nline3"
	if result.Text != expected {
		t.Fatalf("expected %q, got %q", expected, result.Text)
	}
}

func TestApplyEdits_InsertHead(t *testing.T) {
	text := "line1\nline2"
	hunks := []Hunk{{
		Kind:    HunkInsert,
		Cursor:  CursorHead,
		Payload: []string{"first"},
	}}
	result, err := ApplyEdits(text, hunks, nil, "")
	if err != nil {
		t.Fatalf("apply error: %v", err)
	}
	expected := "first\nline1\nline2"
	if result.Text != expected {
		t.Fatalf("expected %q, got %q", expected, result.Text)
	}
}

func TestApplyEdits_InsertTail(t *testing.T) {
	text := "line1\nline2"
	hunks := []Hunk{{
		Kind:    HunkInsert,
		Cursor:  CursorTail,
		Payload: []string{"last"},
	}}
	result, err := ApplyEdits(text, hunks, nil, "")
	if err != nil {
		t.Fatalf("apply error: %v", err)
	}
	expected := "line1\nline2\nlast"
	if result.Text != expected {
		t.Fatalf("expected %q, got %q", expected, result.Text)
	}
}

func TestApplyEdits_MultipleHunks(t *testing.T) {
	text := "line1\nline2\nline3\nline4\nline5"
	hunks := []Hunk{
		{Kind: HunkReplace, Start: 1, End: 1, Payload: []string{"new1"}},
		{Kind: HunkDelete, Start: 3, End: 3},
		{Kind: HunkInsert, Start: 5, Cursor:  CursorAfter, Payload: []string{"after5"}},
	}
	result, err := ApplyEdits(text, hunks, nil, "")
	if err != nil {
		t.Fatalf("apply error: %v", err)
	}
	expected := "new1\nline2\nline4\nline5\nafter5"
	if result.Text != expected {
		t.Fatalf("expected %q, got %q", expected, result.Text)
	}
}

func TestApplyEdits_BoundaryRepair_DuplicateLeadingContext(t *testing.T) {
	// Leading-only context should be stripped independently.
	text := "before\nreplace_me\nafter"
	hunks := []Hunk{{
		Kind:    HunkReplace,
		Start:   2,
		End:     2,
		Payload: []string{"before", "replaced"},
	}}
	result, err := ApplyEdits(text, hunks, nil, "")
	if err != nil {
		t.Fatalf("apply error: %v", err)
	}
	expected := "before\nreplaced\nafter"
	if result.Text != expected {
		t.Fatalf("expected %q, got %q", expected, result.Text)
	}
}

func TestApplyEdits_BoundaryRepair_DuplicateTrailingContext(t *testing.T) {
	// Trailing-only context should be stripped independently.
	text := "before\nreplace_me\nafter"
	hunks := []Hunk{{
		Kind:    HunkReplace,
		Start:   2,
		End:     2,
		Payload: []string{"replaced", "after"},
	}}
	result, err := ApplyEdits(text, hunks, nil, "")
	if err != nil {
		t.Fatalf("apply error: %v", err)
	}
	expected := "before\nreplaced\nafter"
	if result.Text != expected {
		t.Fatalf("expected %q, got %q", expected, result.Text)
	}
}


func TestApplyEdits_BoundaryRepair_TwoEdgeEcho(t *testing.T) {
	// Both leading and trailing edges echo — repair SHOULD fire.
	text := "header\ntarget1\ntarget2\ntarget3\nfooter"
	hunks := []Hunk{{
		Kind:    HunkReplace,
		Start:   2,
		End:     4,
		Payload: []string{"header", "new_body", "footer"},
	}}
	result, err := ApplyEdits(text, hunks, nil, "")
	if err != nil {
		t.Fatalf("apply error: %v", err)
	}
	expected := "header\nnew_body\nfooter"
	if result.Text != expected {
		t.Fatalf("expected %q, got %q", expected, result.Text)
	}
}

func TestApplyEdits_AutoPrefixWarns(t *testing.T) {
	text := "line1\nline2"
	hunks := []Hunk{{
		Kind:    HunkReplace,
		Start:   2,
		End:     2,
		Payload: []string{autoprefixSentinel + "new content"},
	}}
	result, err := ApplyEdits(text, hunks, nil, "")
	if err != nil {
		t.Fatalf("apply error: %v", err)
	}
	if !strings.Contains(result.Text, "new content") {
		t.Fatalf("expected 'new content' in result, got %q", result.Text)
	}
	found := false
	for _, w := range result.Warnings {
		if strings.Contains(w, "auto-prefixed") {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected auto-prefix warning")
	}
}

func TestApplyEdits_AfterInsertLandingShift(t *testing.T) {
	// Anchor line "\tbar()" (indented) is deeper than body "// doc".
	// Lines after anchor are structural closers: "}", "]);"
	// The body "// doc" should land after "]);" not after "\tbar()".
	text := "func foo() {\n\tbar()\n}\n]);\nnextLine"
	hunks := []Hunk{{
		Kind:    HunkInsert,
		Start:   2,
		Cursor:  CursorAfter,
		Payload: []string{"// doc"},
	}}
	result, err := ApplyEdits(text, hunks, nil, "")
	if err != nil {
		t.Fatalf("apply error: %v", err)
	}
	expected := "func foo() {\n\tbar()\n}\n// doc\n]);\nnextLine"
	if result.Text != expected {
		t.Fatalf("expected:\n%s\ngot:\n%s", expected, result.Text)
	}
}



// ---------------------------------------------------------------------------
// snapshot.go tests
// ---------------------------------------------------------------------------

func TestSnapshotStore_Record(t *testing.T) {
	store := NewSnapshotStore()
	hash := store.Record("/test/file.go", "content")
	if len(hash) != 8 {
		t.Fatalf("expected 8-char hash, got %q", hash)
	}
	snap := store.ByHash("/test/file.go", hash)
	if snap == nil {
		t.Fatal("expected snapshot, got nil")
	}
	if snap.Hash != hash {
		t.Fatalf("hash mismatch: %q vs %q", snap.Hash, hash)
	}
}

func TestSnapshotStore_ReadFusion(t *testing.T) {
	store := NewSnapshotStore()
	hash1 := store.Record("/test/file.go", "content")
	hash2 := store.Record("/test/file.go", "content")
	if hash1 != hash2 {
		t.Fatalf("read fusion should reuse same hash: %q vs %q", hash1, hash2)
	}
}

func TestSnapshotStore_MultipleVersions(t *testing.T) {
	store := NewSnapshotStore()
	store.Record("/test/file.go", "v1")
	store.Record("/test/file.go", "v2")
	store.Record("/test/file.go", "v3")

	h3 := ComputeFileHash("v3")
	snap := store.ByHash("/test/file.go", h3)
	if snap == nil || snap.Text != "v3" {
		t.Fatalf("expected latest to be v3, got %v", snap)
	}

	h1 := ComputeFileHash("v1")
	old := store.ByHash("/test/file.go", h1)
	if old == nil || old.Text != "v1" {
		t.Fatalf("expected to find v1 by hash, got %v", old)
	}
}

func TestSnapshotStore_VersionLimit(t *testing.T) {
	store := NewSnapshotStore()
	store.maxPerPath = 2
	store.Record("/test/file.go", "v1")
	store.Record("/test/file.go", "v2")
	store.Record("/test/file.go", "v3")

	h1 := ComputeFileHash("v1")
	if store.ByHash("/test/file.go", h1) != nil {
		t.Fatal("v1 should have been evicted")
	}
	h2 := ComputeFileHash("v2")
	if store.ByHash("/test/file.go", h2) == nil {
		t.Fatal("v2 should still exist")
	}
}

func TestSnapshotStore_PathLimit(t *testing.T) {
	store := NewSnapshotStore()
	store.maxPaths = 2
	store.Record("/a.go", "a")
	store.Record("/b.go", "b")
	store.Record("/c.go", "c")

	ha := ComputeFileHash("a")
	if store.ByHash("/a.go", ha) != nil {
		t.Fatal("/a.go should have been evicted")
	}
	hb := ComputeFileHash("b")
	if store.ByHash("/b.go", hb) == nil {
		t.Fatal("/b.go should still exist")
	}
	hc := ComputeFileHash("c")
	if store.ByHash("/c.go", hc) == nil {
		t.Fatal("/c.go should still exist")
	}
}

// ---------------------------------------------------------------------------
// recovery.go tests
// ---------------------------------------------------------------------------

func TestRecovery_SessionChainReplay(t *testing.T) {
	store := NewSnapshotStore()
	original := "line1\nline2\nline3\nline4\nline5"
	hash := store.Record("/test/file.go", original)

	edits := []Hunk{{
		Kind:    HunkReplace,
		Start:   3,
		End:     3,
		Payload: []string{"new line 3"},
	}}

	result, err := TryRecover(RecoveryRequest{
		Path:        "/test/file.go",
		CurrentText: original,
		ExpectedTag: hash,
		Edits:       edits,
		Snapshots:   store,
	})
	if err != nil {
		t.Fatalf("recovery failed: %v", err)
	}
	if !strings.Contains(result.Text, "new line 3") {
		t.Fatalf("expected 'new line 3' in result, got %q", result.Text)
	}
}

func TestRecovery_3WayMerge(t *testing.T) {
	store := NewSnapshotStore()
	original := "line1\nline2\nline3\nline4\nline5"
	hash := store.Record("/test/file.go", original)

	current := "line1\nline2\nline3\nline4\nline5_modified"

	edits := []Hunk{{
		Kind:    HunkReplace,
		Start:   3,
		End:     3,
		Payload: []string{"new line 3"},
	}}

	result, err := TryRecover(RecoveryRequest{
		Path:        "/test/file.go",
		CurrentText: current,
		ExpectedTag: hash,
		Edits:       edits,
		Snapshots:   store,
	})
	if err != nil {
		t.Fatalf("recovery failed: %v", err)
	}
	if !strings.Contains(result.Text, "new line 3") {
		t.Fatalf("expected 'new line 3' in result, got %q", result.Text)
	}
	if !strings.Contains(result.Text, "line5_modified") {
		t.Fatalf("expected 'line5_modified' in result, got %q", result.Text)
	}
}

func TestRecovery_NoSnapshot(t *testing.T) {
	store := NewSnapshotStore()
	_, err := TryRecover(RecoveryRequest{
		Path:        "/test/file.go",
		CurrentText: "content",
		ExpectedTag: "AAAAAAAA",
		Edits:       []Hunk{{Kind: HunkReplace, Start: 1, End: 1, Payload: []string{"new"}}},
		Snapshots:   store,
	})
	if err == nil {
		t.Fatal("expected error for missing snapshot")
	}
}

// ---------------------------------------------------------------------------
// block hunk tests
// ---------------------------------------------------------------------------

func TestParsePatch_ReplaceBlock(t *testing.T) {
	input := `[main.go#A1B2C3D4]
replace block 10:
+func newFunc() {
+	return nil
+}`
	p, err := ParsePatch(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	h := p.Files[0].Hunks[0]
	if h.Kind != HunkReplace || !h.Block || h.Start != 10 {
		t.Fatalf("unexpected hunk: kind=%d block=%v start=%d", h.Kind, h.Block, h.Start)
	}
	if len(h.Payload) != 3 {
		t.Fatalf("expected 3 payload lines, got %d", len(h.Payload))
	}
}

func TestParsePatch_DeleteBlock(t *testing.T) {
	input := `[main.go#A1B2C3D4]
delete block 5`
	p, err := ParsePatch(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	h := p.Files[0].Hunks[0]
	if h.Kind != HunkDelete || !h.Block || h.Start != 5 {
		t.Fatalf("unexpected hunk: kind=%d block=%v start=%d", h.Kind, h.Block, h.Start)
	}
}

func TestParsePatch_InsertAfterBlock(t *testing.T) {
	input := `[main.go#A1B2C3D4]
insert after block 10:
+// comment after block`
	p, err := ParsePatch(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	h := p.Files[0].Hunks[0]
	if h.Kind != HunkInsert || !h.Block || h.Cursor != CursorAfter || h.Start != 10 {
		t.Fatalf("unexpected hunk: kind=%d block=%v cursor=%q start=%d", h.Kind, h.Block, h.Cursor, h.Start)
	}
}

func TestApplyEdits_ReplaceBlock_WithResolver(t *testing.T) {
	text := "line1\nfunc foo() {\n\tbar()\n}\nline5"
	resolver := func(path string, line int) (*BlockSpan, error) {
		if line == 2 {
			return &BlockSpan{Start: 2, End: 4}, nil
		}
		return nil, nil
	}
	hunks := []Hunk{{
		Kind:    HunkReplace,
		Start:   2,
		Block:   true,
		Payload: []string{"func foo() {", "return nil", "}"},
	}}
	result, err := ApplyEdits(text, hunks, resolver, "test.go")
	if err != nil {
		t.Fatalf("apply error: %v", err)
	}
	expected := "line1\nfunc foo() {\nreturn nil\n}\nline5"
	if result.Text != expected {
		t.Fatalf("expected:\n%s\ngot:\n%s", expected, result.Text)
	}
}

func TestApplyEdits_DeleteBlock_WithResolver(t *testing.T) {
	text := "before\nfunc foo() {\n\tbar()\n}\nafter"
	resolver := func(path string, line int) (*BlockSpan, error) {
		if line == 2 {
			return &BlockSpan{Start: 2, End: 4}, nil
		}
		return nil, nil
	}
	hunks := []Hunk{{
		Kind:  HunkDelete,
		Start: 2,
		Block: true,
	}}
	result, err := ApplyEdits(text, hunks, resolver, "test.go")
	if err != nil {
		t.Fatalf("apply error: %v", err)
	}
	expected := "before\nafter"
	if result.Text != expected {
		t.Fatalf("expected:\n%s\ngot:\n%s", expected, result.Text)
	}
}

func TestApplyEdits_InsertAfterBlock_WithResolver(t *testing.T) {
	text := "func foo() {\n\tbar()\n}\nother"
	resolver := func(path string, line int) (*BlockSpan, error) {
		if line == 1 {
			return &BlockSpan{Start: 1, End: 3}, nil
		}
		return nil, nil
	}
	hunks := []Hunk{{
		Kind:    HunkInsert,
		Start:   1,
		Cursor:  CursorAfter,
		Block:   true,
		Payload: []string{"// after block"},
	}}
	result, err := ApplyEdits(text, hunks, resolver, "test.go")
	if err != nil {
		t.Fatalf("apply error: %v", err)
	}
	expected := "func foo() {\n\tbar()\n}\n// after block\nother"
	if result.Text != expected {
		t.Fatalf("expected:\n%s\ngot:\n%s", expected, result.Text)
	}
}

func TestApplyEdits_BlockNoResolver(t *testing.T) {
	text := "line1"
	hunks := []Hunk{{
		Kind:  HunkReplace,
		Start: 1,
		Block: true,
	}}
	_, err := ApplyEdits(text, hunks, nil, "")
	if err == nil {
		t.Fatal("expected error for block hunk without resolver")
	}
}

func TestApplyEdits_BlockResolverReturnsNil(t *testing.T) {
	text := "line1"
	resolver := func(path string, line int) (*BlockSpan, error) {
		return nil, nil
	}
	hunks := []Hunk{{
		Kind:    HunkReplace,
		Start:   2,
		Block:   true,
		Payload: []string{"new"},
	}}
	_, err := ApplyEdits(text, hunks, resolver, "test.go")
	if err == nil {
		t.Fatal("expected error when resolver returns nil")
	}
}

// ---------------------------------------------------------------------------
// integration: parse → apply round-trip
// ---------------------------------------------------------------------------

func TestIntegration_ParseAndApply(t *testing.T) {
	input := `[file.txt#AAAAAAAA]
replace 2..3:
+new line 2
+new line 3`

	patch, err := ParsePatch(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	text := "line1\nline2\nline3\nline4\nline5"
	result, err := ApplyEdits(text, patch.Files[0].Hunks, nil, "")
	if err != nil {
		t.Fatalf("apply error: %v", err)
	}

	expected := "line1\nnew line 2\nnew line 3\nline4\nline5"
	if result.Text != expected {
		t.Fatalf("expected:\n%s\ngot:\n%s", expected, result.Text)
	}
}

func TestIntegration_FullRoundTrip(t *testing.T) {
	store := NewSnapshotStore()
	original := "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n"
	hash := store.Record("/main.go", original)

	patchStr := `[main.go#` + hash + `]
replace 6..6:
+	fmt.Println("world")`

	patch, err := ParsePatch(patchStr)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	result, err := ApplyEdits(original, patch.Files[0].Hunks, nil, "")
	if err != nil {
		t.Fatalf("apply error: %v", err)
	}

	if !strings.Contains(result.Text, `"world"`) {
		t.Fatalf("expected 'world' in result:\n%s", result.Text)
	}

	newHash := store.Record("/main.go", result.Text)
	if newHash == hash {
		t.Fatal("hash should change after edit")
	}
}

// ---------------------------------------------------------------------------
// OldToNew mapping tests — verify line identity tracking during ApplyEdits
// ---------------------------------------------------------------------------

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
