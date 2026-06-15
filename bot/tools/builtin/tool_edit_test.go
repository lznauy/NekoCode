package builtin

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nekocode/bot/tools"
	"nekocode/bot/tools/hashline"
)

func init() {
	tools.SetGlobalSnapshotStore(hashline.NewSnapshotStore())
}
func TestEdit_InsertHeadEmptyFile(t *testing.T) {
	// Regression: head-insert into an empty file used to panic because
	// the context-after loop started at hunkEnd=0, hitting oldLines[-1].
	td := t.TempDir()
	e := &EditTool{}
	p := filepath.Join(td, "empty.txt")
	os.WriteFile(p, []byte(""), 0644)

	hash := tools.GetGlobalSnapshotStore().Record(p, "")

	patch := fmt.Sprintf(`*** Begin Patch
[%s#%s]
insert head:
+line1
*** End Patch`, p, hash)

	if _, err := e.Execute(context.Background(), map[string]any{"patch": patch}); err != nil {
		t.Fatalf("edit: %v", err)
	}

	data, _ := os.ReadFile(p)
	if string(data) != "line1\n" {
		t.Errorf("expected 'line1\\n', got %q", string(data))
	}
}

func TestEdit_Replace(t *testing.T) {
	td := t.TempDir()
	e := &EditTool{}
	p := filepath.Join(td, "editme.txt")
	os.WriteFile(p, []byte("line1\nline2\nline3\n"), 0644)

	hash := tools.GetGlobalSnapshotStore().Record(p, "line1\nline2\nline3\n")

	patch := fmt.Sprintf(`*** Begin Patch
[%s#%s]
replace 2..2:
+replaced
*** End Patch`, p, hash)

	out, err := e.Execute(context.Background(), map[string]any{"patch": patch})
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

func TestEdit_InsertAfter(t *testing.T) {
	td := t.TempDir()
	e := &EditTool{}
	p := filepath.Join(td, "editme.txt")
	os.WriteFile(p, []byte("line1\nline2\nline3\n"), 0644)

	hash := tools.GetGlobalSnapshotStore().Record(p, "line1\nline2\nline3\n")

	patch := fmt.Sprintf(`*** Begin Patch
[%s#%s]
insert after 2:
+inserted
*** End Patch`, p, hash)

	_, err := e.Execute(context.Background(), map[string]any{"patch": patch})
	if err != nil {
		t.Fatalf("edit: %v", err)
	}
	data, _ := os.ReadFile(p)
	if string(data) != "line1\nline2\ninserted\nline3\n" {
		t.Errorf("unexpected content: %q", string(data))
	}
}

func TestEdit_Delete(t *testing.T) {
	td := t.TempDir()
	e := &EditTool{}
	p := filepath.Join(td, "editme.txt")
	os.WriteFile(p, []byte("line1\nline2\nline3\n"), 0644)

	hash := tools.GetGlobalSnapshotStore().Record(p, "line1\nline2\nline3\n")

	patch := fmt.Sprintf(`*** Begin Patch
[%s#%s]
delete 2
*** End Patch`, p, hash)

	_, err := e.Execute(context.Background(), map[string]any{"patch": patch})
	if err != nil {
		t.Fatalf("edit: %v", err)
	}
	data, _ := os.ReadFile(p)
	if string(data) != "line1\nline3\n" {
		t.Errorf("unexpected content: %q", string(data))
	}
}

func TestEdit_StaleTag(t *testing.T) {
	td := t.TempDir()
	e := &EditTool{}
	p := filepath.Join(td, "editme.txt")
	os.WriteFile(p, []byte("line1\nline2\nline3\n"), 0644)

	// Use a non-existent tag.
	patch := fmt.Sprintf(`*** Begin Patch
[%s#AAAAAAAA]
replace 2..2:
+replaced
*** End Patch`, p)

	_, err := e.Execute(context.Background(), map[string]any{"patch": patch})
	if err == nil {
		t.Fatal("expected error for stale tag")
	}
	if !strings.Contains(err.Error(), "Edit rejected") && !strings.Contains(err.Error(), "no snapshot") {
		t.Errorf("expected stale/snapshot error, got: %v", err)
	}
}

func TestEdit_SequentialEdits(t *testing.T) {
	td := t.TempDir()
	e := &EditTool{}
	p := filepath.Join(td, "seq.txt")
	content := "AA\nBB\nCC\nDD\nEE\nFF\nGG\nHH\n"
	os.WriteFile(p, []byte(content), 0644)

	hash := tools.GetGlobalSnapshotStore().Record(p, content)

	// Edit 1: replace lines 2-3 (BB-CC) with X.
	patch1 := fmt.Sprintf(`*** Begin Patch
[%s#%s]
replace 2..3:
+X
*** End Patch`, p, hash)
	_, err := e.Execute(context.Background(), map[string]any{"patch": patch1})
	if err != nil {
		t.Fatalf("edit 1: %v", err)
	}

	data, _ := os.ReadFile(p)
	if string(data) != "AA\nX\nDD\nEE\nFF\nGG\nHH\n" {
		t.Errorf("after edit 1: %q", string(data))
	}

	// Edit 2: replace DD (now line 3, but use fresh tag).
	hash2 := tools.GetGlobalSnapshotStore().Record(p, string(data))
	patch2 := fmt.Sprintf(`*** Begin Patch
[%s#%s]
replace 3..3:
+ZZZ
*** End Patch`, p, hash2)
	_, err = e.Execute(context.Background(), map[string]any{"patch": patch2})
	if err != nil {
		t.Fatalf("edit 2: %v", err)
	}

	data, _ = os.ReadFile(p)
	if string(data) != "AA\nX\nZZZ\nEE\nFF\nGG\nHH\n" {
		t.Errorf("after edit 2: %q", string(data))
	}
}

func TestEdit_FullFlow(t *testing.T) {
	td := t.TempDir()
	e := &EditTool{}

	var lines []string
	for i := 1; i <= 50; i++ {
		lines = append(lines, fmt.Sprintf("line %02d: some content here", i))
	}
	content := strings.Join(lines, "\n") + "\n"
	p := filepath.Join(td, "big.txt")
	os.WriteFile(p, []byte(content), 0644)

	hash := tools.GetGlobalSnapshotStore().Record(p, content)

	// Replace lines 20-25.
	patch := fmt.Sprintf(`*** Begin Patch
[%s#%s]
replace 20..25:
+new line A
+new line B
*** End Patch`, p, hash)
	_, err := e.Execute(context.Background(), map[string]any{"patch": patch})
	if err != nil {
		t.Fatalf("edit 1 failed: %v", err)
	}

	data, _ := os.ReadFile(p)
	newContent := string(data)
	prefix := strings.Join(lines[:19], "\n") + "\nnew line A\nnew line B\n"
	if !strings.HasPrefix(newContent, prefix) {
		t.Errorf("bad content prefix: %q", newContent[:100])
	}

	// Edit 2: replace "new line B" using fresh tag.
	hash2 := tools.GetGlobalSnapshotStore().Record(p, newContent)
	patch2 := fmt.Sprintf(`*** Begin Patch
[%s#%s]
replace 21..21:
+edited line B
*** End Patch`, p, hash2)
	_, err = e.Execute(context.Background(), map[string]any{"patch": patch2})
	if err != nil {
		t.Fatalf("edit 2 failed: %v", err)
	}
	data, _ = os.ReadFile(p)
	if !strings.Contains(string(data), "edited line B") {
		t.Error("second edit should have replaced new line B")
	}
}

func TestEdit_MultipleFiles(t *testing.T) {
	td := t.TempDir()
	e := &EditTool{}

	aPath := filepath.Join(td, "a.txt")
	bPath := filepath.Join(td, "b.txt")
	os.WriteFile(aPath, []byte("aaa\n"), 0644)
	os.WriteFile(bPath, []byte("bbb\n"), 0644)

	hashA := tools.GetGlobalSnapshotStore().Record(aPath, "aaa\n")
	hashB := tools.GetGlobalSnapshotStore().Record(bPath, "bbb\n")

	patch := fmt.Sprintf(`*** Begin Patch
[%s#%s]
replace 1..1:
+AAA
[%s#%s]
replace 1..1:
+BBB
*** End Patch`, aPath, hashA, bPath, hashB)

	_, err := e.Execute(context.Background(), map[string]any{"patch": patch})
	if err != nil {
		t.Fatalf("edit: %v", err)
	}

	dataA, _ := os.ReadFile(aPath)
	dataB, _ := os.ReadFile(bPath)
	if string(dataA) != "AAA\n" {
		t.Errorf("a.txt: %q", string(dataA))
	}
	if string(dataB) != "BBB\n" {
		t.Errorf("b.txt: %q", string(dataB))
	}
}

func TestEdit_RollbackOnSecondFileFail(t *testing.T) {
	td := t.TempDir()
	e := &EditTool{}

	aPath := filepath.Join(td, "a.txt")
	bPath := filepath.Join(td, "b.txt")
	os.WriteFile(aPath, []byte("old_a\n"), 0644)
	os.WriteFile(bPath, []byte("old_b\n"), 0644)

	hashA := tools.GetGlobalSnapshotStore().Record(aPath, "old_a\n")
	hashB := tools.GetGlobalSnapshotStore().Record(bPath, "old_b\n")

	// File A: valid edit. File B: bad line number (999) → apply fails.
	patch := fmt.Sprintf(`*** Begin Patch
[%s#%s]
replace 1..1:
+new_a
[%s#%s]
replace 999..999:
+new_b
*** End Patch`, aPath, hashA, bPath, hashB)

	_, err := e.Execute(context.Background(), map[string]any{"patch": patch})
	if err == nil {
		t.Fatal("expected error for file B, got nil")
	}

	// File A must be rolled back to original content.
	dataA, _ := os.ReadFile(aPath)
	if string(dataA) != "old_a\n" {
		t.Errorf("rollback failed: a.txt got %q, want 'old_a\\n'", string(dataA))
	}
	// File B untouched.
	dataB, _ := os.ReadFile(bPath)
	if string(dataB) != "old_b\n" {
		t.Errorf("b.txt modified: %q", string(dataB))
	}
}


func TestEdit_Revert(t *testing.T) {
	td := t.TempDir()
	e := &EditTool{}

	filePath := filepath.Join(td, "revert_test.txt")
	original := "line one\nline two\nline three\n"
	os.WriteFile(filePath, []byte(original), 0644)

	tag := tools.GetGlobalSnapshotStore().Record(filePath, hashline.NormalizeToLF(original))

	// Edit the file (this also saves a .pre-edit snapshot).
	_, err := e.Execute(context.Background(), map[string]any{
		"patch": fmt.Sprintf("[%s#%s]\nreplace 1..1:\n+modified line one\n", filePath, tag),
	})
	if err != nil {
		t.Fatalf("edit failed: %v", err)
	}

	// Revert via revert=true.
	out, err := e.Execute(context.Background(), map[string]any{
		"patch":  filePath,
		"revert": true,
	})
	if err != nil {
		t.Fatalf("revert failed: %v", err)
	}
	if !strings.Contains(out, "Reverted") {
		t.Errorf("expected 'Reverted' in output, got %q", out)
	}
	if data, _ := os.ReadFile(filePath); string(data) != original {
		t.Errorf("after revert: %q, want %q", string(data), original)
	}
}
