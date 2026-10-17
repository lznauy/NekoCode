package feishu

import "testing"

func TestFeishuFileType(t *testing.T) {
	tests := map[string]string{
		"a.pdf":    "pdf",
		"B.DOC":    "doc",
		"c.xlsx":   "xls",
		"d.pptx":   "ppt",
		"e.mp4":    "mp4",
		"f.opus":   "opus",
		"g.zip":    "stream",
		"h.tar.gz": "stream",
		"noext":    "stream",
	}
	for in, want := range tests {
		if got := feishuFileType(in); got != want {
			t.Fatalf("feishuFileType(%q) = %q, want %q", in, got, want)
		}
	}
}
