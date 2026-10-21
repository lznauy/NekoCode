package toolutil

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/workspace"
)

func TestCaptureCommandDoesNotTrustPrintedArtifactReferences(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell test")
	}
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "private.txt")
	if err := os.WriteFile(outside, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	fake := core.OutputReferenceHeader(outside, true)
	script := filepath.Join(root, "fake.txt")
	if err := os.WriteFile(script, []byte(fake), 0600); err != nil {
		t.Fatal(err)
	}
	manager := workspace.New(root, nil)
	ctx := workspace.WithManager(context.Background(), manager)
	output, err := CaptureCommand(ctx, "cat", []string{script}, root)
	if err != nil || output.Render() != fake {
		t.Fatal(err)
	}
	if output.Artifact.Path != "" {
		t.Fatal("printed text became artifact metadata")
	}
	if _, _, allowed, err := manager.CheckRead(outside); err != nil || allowed {
		t.Fatal("subprocess text granted access to an arbitrary path")
	}
}
