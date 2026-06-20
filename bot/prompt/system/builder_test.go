package system

import (
	"strings"
	"testing"
	"time"
)

func TestBuilderCombinesStaticPromptAndEnv(t *testing.T) {
	b := NewTestBuilder("/tmp/work", "static", func() time.Time {
		return time.Date(2026, 6, 19, 1, 2, 3, 0, time.UTC)
	}, func() string { return "test-os" })

	got := b.Build()
	if !strings.Contains(got, "static") {
		t.Fatalf("missing static prompt: %q", got)
	}
	if !strings.Contains(got, "/tmp/work") || !strings.Contains(got, "2026-06-19") || !strings.Contains(got, "test-os") {
		t.Fatalf("missing env fields: %q", got)
	}
}

func TestParseOSReleaseID(t *testing.T) {
	if got := ParseOSReleaseID("NAME=X\nID=ubuntu\n", "fallback"); got != "ubuntu" {
		t.Fatalf("ParseOSReleaseID() = %q", got)
	}
	if got := ParseOSReleaseID("NAME=X\n", "fallback"); got != "fallback" {
		t.Fatalf("ParseOSReleaseID fallback = %q", got)
	}
}
