package wecom

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestMessageChunksPreserveUnusualMarkdown(t *testing.T) {
	for _, text := range []string{
		strings.Repeat("`", 65) + "\nbody",
		"```" + strings.Repeat("x", maxTextBytes) + "\nbody\n```",
		"```go\n" + strings.Repeat("猫🐱 code\n", 5000) + "```\nafter",
	} {
		parts, err := splitMessage(text, maxTextBytes)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Join(parts, "") != text {
			t.Fatal("message content changed during delivery")
		}
		for _, part := range parts {
			if len(part) > maxTextBytes || !utf8.ValidString(part) {
				t.Fatal("invalid message part")
			}
		}
	}
}

func TestMarkdownChunksPreservePlainText(t *testing.T) {
	for _, text := range []string{"", "short unchanged", strings.Repeat("猫🐱", 20000), strings.Repeat("line\n", 10000)} {
		parts, err := splitMessage(text, maxTextBytes)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Join(parts, "") != text {
			t.Fatal("plain text lost or changed")
		}
		for _, part := range parts {
			if len(part) > maxTextBytes || !utf8.ValidString(part) {
				t.Fatalf("invalid part: bytes=%d", len(part))
			}
		}
	}
}

func TestMessageChunksPreferNewlines(t *testing.T) {
	parts, err := splitMessage("first\nsecond", 8)
	if err != nil || len(parts) != 2 || parts[0] != "first\n" || parts[1] != "second" {
		t.Fatalf("parts=%q err=%v", parts, err)
	}
}
