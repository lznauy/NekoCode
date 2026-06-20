package editdsl

import (
	"strings"
	"testing"
)

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
