package connect

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalImagePaths(t *testing.T) {
	dir := t.TempDir()
	exist := func(name string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	chart := exist("chart.png")
	photo := exist("photo.jpg")
	missing := filepath.Join(dir, "gone.png")

	text := "图表保存在 " + chart + "，照片见 " + photo + "。缺失的 " + missing + " 和目录 " + dir + " 应被忽略。"
	got := LocalImagePaths(text)
	if len(got) != 2 || got[0] != chart || got[1] != photo {
		t.Fatalf("LocalImagePaths = %v, want [%s %s]", got, chart, photo)
	}

	// Relative paths and URLs never match.
	if p := LocalImagePaths("see ./out.png and https://x.test/a.png"); p != nil {
		t.Fatalf("relative/url paths should not match: %v", p)
	}
	// Duplicates collapse.
	dup := "one " + chart + " two " + chart
	if p := LocalImagePaths(dup); len(p) != 1 || p[0] != chart {
		t.Fatalf("duplicate handling = %v", p)
	}
	// Cap at maxOutboundImages.
	var many []string
	for i := 0; i < maxOutboundImages+2; i++ {
		many = append(many, exist(strings.Repeat("a", i)+"x.png"))
	}
	if p := LocalImagePaths(strings.Join(many, " ")); len(p) != maxOutboundImages {
		t.Fatalf("cap = %d, want %d", len(p), maxOutboundImages)
	}
}
