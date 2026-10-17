package acp

import (
	"encoding/base64"
	"os"
	"strings"
	"testing"

	"nekocode/util/attachment"
)

func TestPromptContent(t *testing.T) {
	text, images, err := promptContent([]contentBlock{
		{Type: "text", Text: "Review this"},
		{Type: "resource_link", Name: "main.go", URI: "file:///repo/main.go", Description: "entrypoint"},
	}, "session-a")
	if err != nil {
		t.Fatal(err)
	}
	want := "Review this\n\n[main.go](file:///repo/main.go) — entrypoint"
	if text != want {
		t.Fatalf("text = %q, want %q", text, want)
	}
	if len(images) != 0 {
		t.Fatalf("images = %#v", images)
	}
}

func TestPromptContentStoresImageBlocks(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	// A minimal PNG header so attachment.SaveImage accepts the payload.
	png := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1A, '\n'}
	text, images, err := promptContent([]contentBlock{
		{Type: "text", Text: "看看"},
		{Type: "image", Data: base64.StdEncoding.EncodeToString(png)},
	}, "20261016T000000-000000001")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "[Image #1]") {
		t.Fatalf("text = %q, want image marker", text)
	}
	if len(images) != 1 || images[0].Label != "[Image #1]" {
		t.Fatalf("images = %#v", images)
	}
	if data, err := os.ReadFile(images[0].Path); err != nil || data == nil {
		t.Fatalf("stored image unreadable: %v", err)
	}
	if dir, err := attachment.ImageDir("20261016T000000-000000001"); err != nil {
		t.Fatal(err)
	} else if !strings.HasPrefix(images[0].Path, dir+string(os.PathSeparator)) {
		t.Fatalf("image path = %q, want under %s", images[0].Path, dir)
	}
}

func TestPromptContentRejectsBadImages(t *testing.T) {
	for name, block := range map[string]contentBlock{
		"missing data":   {Type: "image"},
		"invalid base64": {Type: "image", Data: "!!!not-base64!!!"},
	} {
		if _, _, err := promptContent([]contentBlock{block}, "session-a"); err == nil {
			t.Fatalf("%s: image block should be rejected", name)
		}
	}
}
