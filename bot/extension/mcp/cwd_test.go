package mcp

import (
	"context"
	"path/filepath"
	"testing"
)

func TestClientUsesConfiguredWorkingDirectory(t *testing.T) {
	cmd, _ := startMockMCP(t, nil)
	dir := t.TempDir()
	c := newClient("cwd", ServerConfig{Command: cmd.Path, CWD: dir})
	defer c.Close()
	if err := c.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, err := c.CallTool(context.Background(), "cwd", nil)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.EvalSymlinks(dir)
	if got != want {
		t.Fatalf("child cwd=%q, want %q", got, want)
	}
}
