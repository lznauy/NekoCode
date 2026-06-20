package editdsl

import (
	"testing"
)

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
