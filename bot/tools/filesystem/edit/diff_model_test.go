package edit

import "testing"

func TestStructuredDiffFromNumberedPreviewKeepsLineNumbers(t *testing.T) {
	model := structuredDiffFromText(" 1:one\n-2:two\n+2:TWO\n", "file.txt")
	if len(model.Lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(model.Lines))
	}
	if model.Lines[1].Kind != "del" || model.Lines[1].LineNo != 2 || model.Lines[1].Text != "two" {
		t.Fatalf("bad delete line: %+v", model.Lines[1])
	}
	if model.Lines[2].Kind != "add" || model.Lines[2].LineNo != 2 || model.Lines[2].Text != "TWO" {
		t.Fatalf("bad add line: %+v", model.Lines[2])
	}
}
