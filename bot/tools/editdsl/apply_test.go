package editdsl

import (
	"strings"
	"testing"
)

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
		{Kind: HunkInsert, Start: 5, Cursor: CursorAfter, Payload: []string{"after5"}},
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
