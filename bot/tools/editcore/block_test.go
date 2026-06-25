package editcore

import (
	"testing"
)

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
