package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeProjectFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestOptionalProjectFilesAndNoParentDiscovery(t *testing.T) {
	root := t.TempDir()
	writeProjectFile(t, filepath.Join(root, "NEKOCODE.md"), "parent rules")
	child := filepath.Join(root, "child")
	if err := os.Mkdir(child, 0o755); err != nil {
		t.Fatal(err)
	}
	p, err := New(child)
	if err != nil {
		t.Fatal(err)
	}
	if errors := p.Refresh(); len(errors) != 0 || p.Instructions != "" || len(p.Servers) != 0 {
		t.Fatalf("unexpected inherited config: %+v, %v", p, errors)
	}
	if _, err := os.Stat(filepath.Join(child, ".nekocode")); !os.IsNotExist(err) {
		t.Fatalf("read created directory: %v", err)
	}
	writeProjectFile(t, p.InstructionsPath(), "\ufeffchild rules\n")
	if errors := p.Refresh(); len(errors) != 0 || p.Instructions != "child rules" {
		t.Fatalf("standalone rules: %+v %v", p, errors)
	}
}

func TestProjectMCPPathsAndDefaults(t *testing.T) {
	p, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	writeProjectFile(t, p.MCPPath(), `{"mcpServers":{
		"local":{"command":"./server","cwd":"tools","args":["./literal"," space "],"env":{"VALUE":" untouched "}},
		"path":{"command":"node"},"off":{"enabled":false}
	}}`)
	if errors := p.Refresh(); len(errors) != 0 {
		t.Fatal(errors)
	}
	local := p.Servers["local"]
	if !local.Enabled || local.CWD != filepath.Join(p.Root, "tools") || local.Command != filepath.Join(p.Root, "tools", "server") {
		t.Fatalf("bad local: %+v", local)
	}
	if local.Args[0] != "./literal" || local.Args[1] != " space " || local.Env["VALUE"] != " untouched " {
		t.Fatalf("rewritten values: %+v", local)
	}
	if p.Servers["path"].CWD != p.Root || p.Servers["path"].Command != "node" || p.Servers["off"].Enabled {
		t.Fatalf("bad defaults: %+v", p.Servers)
	}
}

func TestRefreshRetainsInvalidFileAndRemovesDeletedFile(t *testing.T) {
	p, _ := New(t.TempDir())
	writeProjectFile(t, p.InstructionsPath(), "old rules")
	writeProjectFile(t, p.MCPPath(), `{"mcpServers":{"one":{"command":"node"}}}`)
	if errors := p.Refresh(); len(errors) != 0 {
		t.Fatal(errors)
	}
	for _, bad := range []string{
		`{`, `null`, `{}`, `{"mcp_servers":{}}`, `{"mcpServers":{"one":{}}}`, `{"mcpServers":{" bad ":{"command":"node"}}}`,
		`{"mcpServers":{"one":{"command":"node\u0000"}}}`,
		`{"mcpServers":{"one":{"command":"node","args":["\u0000"]}}}`,
		`{"mcpServers":{"one":{"command":"node","cwd":"\u0000"}}}`,
		`{"mcpServers":{"one":{"command":"node","env":{"KEY":"\u0000"}}}}`,
	} {
		writeProjectFile(t, p.MCPPath(), bad)
		writeProjectFile(t, p.InstructionsPath(), "new rules")
		if errors := p.Refresh(); len(errors) != 1 || p.Servers["one"].Command != "node" || p.Instructions != "new rules" {
			t.Fatalf("refresh %q: %+v %v", bad, p, errors)
		}
		if !strings.Contains(p.Diagnostics[0], p.MCPPath()) {
			t.Fatal("missing source path")
		}
	}
	writeProjectFile(t, p.InstructionsPath(), strings.Repeat("x", MaxInstructionsBytes+1))
	if errors := p.Refresh(); len(errors) != 2 || p.Instructions != "new rules" {
		t.Fatalf("oversized rules replaced last good: %v", errors)
	}
	for _, path := range []string{p.InstructionsPath(), p.MCPPath()} {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
	if errors := p.Refresh(); len(errors) != 0 || p.Instructions != "" || len(p.Servers) != 0 || len(p.Diagnostics) != 0 {
		t.Fatalf("deleted files still active: %+v %v", p, errors)
	}
}
