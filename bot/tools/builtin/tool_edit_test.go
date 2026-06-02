package builtin

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nekocode/bot/tools"
)

func TestEditHashline(t *testing.T) {
	td := t.TempDir()
	e := &EditTool{}
	p := filepath.Join(td, "editme.txt")
	os.WriteFile(p, []byte("line1\nline2\nline3\n"), 0644)

	// Hashline: replace line 2.
	h := tools.HashLine("line2")
	out, err := e.Execute(context.Background(), map[string]any{
		"path":       p,
		"hashes":     []any{"2:" + h},
		"new_string": "replaced",
	})
	if err != nil {
		t.Fatalf("edit: %v", err)
	}
	if out == "" {
		t.Error("empty output")
	}
	data, _ := os.ReadFile(p)
	if string(data) != "line1\nreplaced\nline3\n" {
		t.Errorf("unexpected content: %q", string(data))
	}
}

func TestEditHashline_Stale(t *testing.T) {
	td := t.TempDir()
	e := &EditTool{}
	p := filepath.Join(td, "editme.txt")
	os.WriteFile(p, []byte("line1\nline2\nline3\n"), 0644)

	_, err := e.Execute(context.Background(), map[string]any{
		"path":   p,
		"hashes": []any{"2:xx"}, // non-existent hash
	})
	if err == nil {
		t.Fatal("expected stale error")
	}
	if !strings.Contains(err.Error(), "Hashline stale") {
		t.Errorf("expected stale error, got: %v", err)
	}
}

func TestEditHashline_InsertAfter(t *testing.T) {
	td := t.TempDir()
	e := &EditTool{}
	p := filepath.Join(td, "editme.txt")
	os.WriteFile(p, []byte("line1\nline2\nline3\n"), 0644)

	h := tools.HashLine("line2")
	_, err := e.Execute(context.Background(), map[string]any{
		"path":       p,
		"hashes":     []any{"2:" + h},
		"new_string": "inserted",
		"op":         "insert_after",
	})
	if err != nil {
		t.Fatalf("edit: %v", err)
	}
	data, _ := os.ReadFile(p)
	if string(data) != "line1\nline2\ninserted\nline3\n" {
		t.Errorf("unexpected content: %q", string(data))
	}
}

func TestEditHashline_Delete(t *testing.T) {
	td := t.TempDir()
	e := &EditTool{}
	p := filepath.Join(td, "editme.txt")
	os.WriteFile(p, []byte("line1\nline2\nline3\n"), 0644)

	h := tools.HashLine("line2")
	_, err := e.Execute(context.Background(), map[string]any{
		"path":   p,
		"hashes": []any{"2:" + h},
		"op":     "delete",
	})
	if err != nil {
		t.Fatalf("edit: %v", err)
	}
	data, _ := os.ReadFile(p)
	if string(data) != "line1\nline3\n" {
		t.Errorf("unexpected content: %q", string(data))
	}
}

func TestEditHashline_SequentialEdits(t *testing.T) {
	td := t.TempDir()
	e := &EditTool{}
	p := filepath.Join(td, "seq.txt")
	os.WriteFile(p, []byte("AA\nBB\nCC\nDD\nEE\nFF\nGG\nHH\n"), 0644)

	// Edit 1: replace lines 2-3 (BB-CC) with 2 lines.
	h2 := tools.HashLine("BB")
	h3 := tools.HashLine("CC")
	_, err := e.Execute(context.Background(), map[string]any{
		"path": p, "hashes": []any{"2:" + h2, "3:" + h3},
		"new_string": "X", // replace 2 lines with 1 → shifts line numbers
	})
	if err != nil {
		t.Fatalf("edit 1: %v", err)
	}

	// Edit 2: replace line "DD" using its hash. Still valid — same content, same hash.
	hDD := tools.HashLine("DD")
	_, err = e.Execute(context.Background(), map[string]any{
		"path": p, "hashes": []any{"4:" + hDD},
		"new_string": "ZZZ",
	})
	if err != nil {
		t.Fatalf("edit 2 (after edit 1 changed line count): %v", err)
	}

	data, _ := os.ReadFile(p)
	if string(data) != "AA\nX\nZZZ\nEE\nFF\nGG\nHH\n" {
		t.Errorf("got %q", string(data))
	}
}

func TestEditHashline_TrailingSeparator(t *testing.T) {
	// Regression: AI may pass "9:___|", "9:___│", or "9:[___]" (hash+separator/brackets)
	// for empty lines. The code must strip brackets and legacy separators before lookup.
	td := t.TempDir()
	e := &EditTool{}
	p := filepath.Join(td, "readme.txt")
	content := "<!--\n\nline2\n\nline4\n"
	os.WriteFile(p, []byte(content), 0644)

	h := tools.HashLine("") // ___ for empty line

	// Legacy separator |
	_, err := e.Execute(context.Background(), map[string]any{
		"path":   p,
		"hashes": []any{"2:" + h + "|"},
		"op":     "delete",
	})
	if err != nil {
		t.Fatalf("trailing | should not cause stale: %v", err)
	}
	os.WriteFile(p, []byte(content), 0644)

	// Legacy separator │
	_, err = e.Execute(context.Background(), map[string]any{
		"path":   p,
		"hashes": []any{"2:" + h + "│"},
		"op":     "delete",
	})
	if err != nil {
		t.Fatalf("trailing │ should not cause stale: %v", err)
	}
	os.WriteFile(p, []byte(content), 0644)

	// Bracket format [___]
	_, err = e.Execute(context.Background(), map[string]any{
		"path":   p,
		"hashes": []any{"2:[" + h + "]"},
		"op":     "delete",
	})
	if err != nil {
		t.Fatalf("bracketed hash [%s] should not cause stale: %v", h, err)
	}
}

func TestEditHashline_CollisionResistant(t *testing.T) {
	td := t.TempDir()
	e := &EditTool{}
	p := filepath.Join(td, "collision.txt")
	// Multiple "}" lines — same content, same hash.
	os.WriteFile(p, []byte("foo\n}\nbar\n}\nbaz\n"), 0644)

	// Target the SECOND "}" (line 4), not the first (line 2).
	h := tools.HashLine("}")
	_, err := e.Execute(context.Background(), map[string]any{
		"path": p, "hashes": []any{"4:" + h},
		"new_string": "changed",
	})
	if err != nil {
		t.Fatalf("collision edit: %v", err)
	}
	data, _ := os.ReadFile(p)
	// Line 4 should be "changed", line 2 should still be "}".
	if string(data) != "foo\n}\nbar\nchanged\nbaz\n" {
		t.Errorf("collision failed: %q", string(data))
	}
}

func TestEditHashline_FullFlow(t *testing.T) {
	td := t.TempDir()
	e := &EditTool{}

	var lines []string
	for i := 1; i <= 50; i++ {
		lines = append(lines, fmt.Sprintf("line %02d: some content here", i))
	}
	content := strings.Join(lines, "\n")
	p := filepath.Join(td, "big.txt")
	os.WriteFile(p, []byte(content), 0644)

	h20 := tools.HashLine(lines[19])
	h25 := tools.HashLine(lines[24])

	_, err := e.Execute(context.Background(), map[string]any{
		"path": p, "hashes": []any{"20:" + h20, "25:" + h25},
		"new_string": "new line A\nnew line B",
	})
	if err != nil {
		t.Fatalf("edit 1 failed: %v", err)
	}

	data, _ := os.ReadFile(p)
	newContent := string(data)
	prefix := strings.Join(lines[:19], "\n") + "\nnew line A\nnew line B\n"
	if !strings.HasPrefix(newContent, prefix) {
		t.Errorf("bad content prefix: %q", newContent[:100])
	}

	hNewB := tools.HashLine("new line B")
	_, err = e.Execute(context.Background(), map[string]any{
		"path": p, "hashes": []any{"21:" + hNewB},
		"new_string": "edited line B",
	})
	if err != nil {
		t.Fatalf("edit 2 (fresh hashes) failed: %v", err)
	}
	data, _ = os.ReadFile(p)
	if !strings.Contains(string(data), "edited line B") {
		t.Error("second edit should have replaced new line B")
	}
}