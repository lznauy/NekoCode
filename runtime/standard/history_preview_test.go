package standard

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestHistoryPreviewBoundsUTF8(t *testing.T) {
	source := strings.Repeat("中", 100000)
	preview := historyPreview(source)
	if !utf8.ValidString(preview) || len(preview) > 66000 || !strings.Contains(preview, "/export") {
		t.Fatal("unbounded or invalid preview")
	}
	if historyPreview("small") != "small" {
		t.Fatal("small content changed")
	}
}
