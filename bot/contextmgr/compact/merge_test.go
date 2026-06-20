package compact

import (
	"strings"
	"testing"
)

func TestBuildMergePrompt(t *testing.T) {
	p := buildMergePrompt("old summary", "new summary")
	if !strings.Contains(p, "old summary") || !strings.Contains(p, "new summary") {
		t.Error("merge prompt should contain both summaries")
	}
	if !strings.Contains(p, "MERGED:") {
		t.Error("merge prompt should end with MERGED:")
	}
}
