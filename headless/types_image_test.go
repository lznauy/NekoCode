package headless

import (
	"encoding/json"
	"testing"
)

func mustFrame(t *testing.T, content string) frame {
	t.Helper()
	var f frame
	raw := `{"type":"user","uuid":"u1","message":{"role":"user","content":` + content + `},"shouldQuery":true}`
	if err := json.Unmarshal([]byte(raw), &f); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestUserParsesImageBlocks(t *testing.T) {
	f := mustFrame(t, `[
		{"type":"text","text":"看看这个"},
		{"type":"image","path":"uploads/shot.jpg"},
		{"type":"text","text":"再配合说明"}
	]`)
	input, err := f.user()
	if err != nil {
		t.Fatal(err)
	}
	want := "看看这个\n\n[Image #1]\n\n再配合说明"
	if input.text != want {
		t.Fatalf("text = %q, want %q", input.text, want)
	}
	if len(input.images) != 1 || input.images[0].Label != "[Image #1]" || input.images[0].Path != "uploads/shot.jpg" {
		t.Fatalf("images = %#v", input.images)
	}
}

func TestUserPlainStringUnchanged(t *testing.T) {
	f := mustFrame(t, `"just text"`)
	input, err := f.user()
	if err != nil {
		t.Fatal(err)
	}
	if input.text != "just text" || len(input.images) != 0 {
		t.Fatalf("input = %#v", input)
	}
}

func TestUserRejectsBadBlocks(t *testing.T) {
	for name, content := range map[string]string{
		"unknown type":  `[{"type":"audio","path":"x"}]`,
		"image no path": `[{"type":"image"}]`,
	} {
		f := mustFrame(t, content)
		if _, err := f.user(); err == nil {
			t.Fatalf("%s: user() should reject %s", name, content)
		}
	}
}

func TestResolveWithinCWD(t *testing.T) {
	base := "/app/workspace/chat-1"
	// Relative paths anchor at the working directory.
	if got, err := resolveWithinCWD(base, "uploads/shot.jpg"); err != nil || got != base+"/uploads/shot.jpg" {
		t.Fatalf("relative resolve = %q err=%v", got, err)
	}
	// Absolute paths inside the base pass through.
	if got, err := resolveWithinCWD(base, base+"/img.png"); err != nil || got != base+"/img.png" {
		t.Fatalf("absolute inside = %q err=%v", got, err)
	}
	// Escapes are rejected — relative and absolute alike.
	for name, path := range map[string]string{
		"traversal":  "../../../etc/passwd",
		"abs escape": "/etc/passwd",
		"empty":      "  ",
	} {
		if _, err := resolveWithinCWD(base, path); err == nil {
			t.Fatalf("%s: path %q must be rejected", name, path)
		}
	}
}
