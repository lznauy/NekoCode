package core

import (
	"nekocode/util/secrets"
	"os"
	"strings"
	"testing"
)

func TestCredentialOutputRedactedBeforePreviewAndArtifact(t *testing.T) {
	key := "credential-output-sentinel-123456789"
	secrets.Register(key)
	store := NewOutputStore(t.TempDir())
	out, err := store.BoundOutput(ToolOutput{Text: key}, MaxOutputBytes)
	if err != nil || strings.Contains(out.Text, key) {
		t.Fatalf("raw preview: %v", err)
	}
	path, err := store.PreserveOutput(key)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil || strings.Contains(string(b), key) {
		t.Fatal("raw preserved output")
	}
	capture := NewOutputCapture(store, MaxCapturedOutputBytes, nil)
	_, _ = capture.Write([]byte(strings.Repeat("line\n", MaxOutputBytes)))
	_, _ = capture.Write([]byte(key[:13]))
	_, _ = capture.Write([]byte(key[13:]))
	output, err := capture.Finish()
	if err != nil {
		t.Fatal(err)
	}
	b, err = os.ReadFile(output.Artifact.Path)
	if err != nil || strings.Contains(string(b), key) || !strings.Contains(string(b), secrets.Replacement) {
		t.Fatalf("raw captured output: %v", err)
	}
}
