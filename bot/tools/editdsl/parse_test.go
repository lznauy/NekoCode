package editdsl

import (
	"strings"
	"testing"
)

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
