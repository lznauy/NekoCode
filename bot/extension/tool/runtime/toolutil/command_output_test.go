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

func TestCaptureCommandGrantsOnlyItsOutputFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses cat")
	}
	manager := workspace.New(t.TempDir(), nil)
	ctx := workspace.WithManager(t.Context(), manager)
	ctx = core.WithOutputStore(ctx, core.NewOutputStore(t.TempDir()))
	input := filepath.Join(manager.Primary(), "input")
	if err := os.WriteFile(input, make([]byte, core.MaxOutputBytes+1), 0600); err != nil {
		t.Fatal(err)
	}
	output, err := CaptureCommand(ctx, "cat", []string{input}, manager.Primary())
	if err != nil || output.Artifact.Path == "" {
		t.Fatalf("missing capture: %v", err)
	}
	path := output.Artifact.Path
	if _, root, allowed, err := manager.CheckRead(path); err != nil || !allowed || !root.FileOnly || root.Access != workspace.AccessReadOnly {
		t.Fatalf("unexpected capture grant: %+v, %v", root, err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if _, _, allowed, err := manager.CheckRead(filepath.Join(path, "entry")); err != nil || allowed {
		t.Fatalf("unexpected descendant access: %t, %v", allowed, err)
	}
}
