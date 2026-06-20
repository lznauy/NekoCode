package plugin

import "testing"

func TestMatchToolRegexAndInvalidPattern(t *testing.T) {
	if !matchTool("web_.*", "web_search") {
		t.Fatal("regex matcher should match web_search")
	}
	if matchTool("[", "read") {
		t.Fatal("invalid regex matcher should not match")
	}
}

func TestMatchTool(t *testing.T) {
	if !matchTool("", "anytool") {
		t.Error("empty matcher should match all")
	}
	if !matchTool(".*", "anytool") {
		t.Error(".* should match all")
	}
	if !matchTool("read", "read") {
		t.Error("exact name should match")
	}
	if !matchTool("read|write", "write") {
		t.Error("alt pattern should match")
	}
	if matchTool("read", "write") {
		t.Error("exact name should not mismatch")
	}
}
