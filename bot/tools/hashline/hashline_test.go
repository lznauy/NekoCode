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
	if h.Kind != HunkInsert || h.Cursor != "after" || h.Start != 20 {
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
	if h.Kind != HunkInsert || h.Cursor != "head" {
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
		Cursor:  "after",
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
		Cursor:  "before",
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
		Cursor:  "head",
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
		Cursor:  "tail",
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
		{Kind: HunkInsert, Start: 5, Cursor: "after", Payload: []string{"after5"}},
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
		Cursor:  "after",
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
	if h.Kind != HunkInsert || !h.Block || h.Cursor != "after" || h.Start != 10 {
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
		Cursor:  "after",
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
