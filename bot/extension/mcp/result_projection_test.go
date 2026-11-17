package mcp

import (
	"strings"
	"testing"
)

func TestMCPResultPreservesStructuredDataAndBlockBoundaries(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want []string
	}{
		{`{"structuredContent":{"id":"saved-1"},"content":[]}`, []string{"saved-1"}},
		{`{"content":[{"type":"resource_link","uri":"file:///report.txt","name":"report"}]}`, []string{"file:///report.txt", "report"}},
		{`{"content":[{"type":"text","text":"scope: A"},{"type":"text","text":"B"}]}`, []string{"scope: A\n\nB"}},
		{`{"content":[{"type":"image","mimeType":"image/png","data":"aGVsbG8="}]}`, []string{"image/png", "aGVsbG8="}},
	} {
		got, err := parseToolCallResult([]byte(tc.raw))
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range tc.want {
			if !strings.Contains(got, want) {
				t.Errorf("lost result %q: %q", want, got)
			}
		}
	}
}
func TestMCPStructuredToolErrorPreservesDetails(t *testing.T) {
	out, err := parseToolCallResult([]byte(`{"isError":true,"structuredContent":{"reason":"partial-save","saved_id":"one"}}`))
	if err == nil || !strings.Contains(out, "partial-save") || !strings.Contains(err.Error(), "one") {
		t.Fatalf("error data lost: %q %v", out, err)
	}
}
