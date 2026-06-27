package guiapp

import (
	"strings"
	"testing"

	"nekocode/common"
)

func TestCompactConfirmArgsEditUsesV2Fields(t *testing.T) {
	req := common.NewConfirmRequest("edit", map[string]any{
		"path":       "/tmp/file.go",
		"oldString":  strings.Repeat("a", 250),
		"newString":  "next",
		"replaceAll": true,
		"patch":      "legacy",
		"_preview":   "diff",
	}, common.LevelWrite)

	got := compactConfirmArgs(req)
	if got["path"] != "/tmp/file.go" {
		t.Fatalf("path = %v", got["path"])
	}
	if _, ok := got["patch"]; ok {
		t.Fatalf("legacy patch should not be exposed: %#v", got)
	}
	if got["replaceAll"] != true {
		t.Fatalf("replaceAll = %v", got["replaceAll"])
	}
	old, _ := got["oldString"].(string)
	if len(old) != 203 || !strings.HasSuffix(old, "...") {
		t.Fatalf("oldString was not truncated: len=%d value=%q", len(old), old)
	}
}
