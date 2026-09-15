package agentprofile

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func knownTool(name string) bool {
	return slices.Contains(append(Builtins()[0].Tools, "capability"), name)
}

func TestAgentMDValidation(t *testing.T) {
	for _, tc := range []struct {
		name, fields, wantErr string
		tools                 []string
	}{
		{"sequence", "tools: [Read, Grep, Bash, Read]", "", []string{"read", "grep", "shell"}},
		{"scalar", "tools: Read, Grep, Bash", "", []string{"read", "grep", "shell"}},
		{"base", "base: explore", "", Builtins()[1].Tools},
		{"empty", "base: coder\ntools: []", "", []string{}},
		{"no tools", "description: text only", "", nil},
		{"unknown", "tools: [reed]", "unknown agent tool", nil},
		{"nested", "tools: [task]", "reserved", nil},
		{"lifecycle", "tools: [nekocode_submit_result]", "reserved", nil},
		{"expansion", "base: explore\ntools: [Bash]", "exceeds base", nil},
		{"unknown base", "base: root", "unknown base", nil},
		{"limit", "max_steps: 51", "max_steps", nil},
		{"negative", "max_steps: -1", "max_steps", nil},
		{"ignored field", "permissionMode: bypassPermissions", "field permissionMode", nil},
		{"typo", "tool: [Read]", "field tool", nil},
		{"mcp", "tools: [capability, mcp__demo__lookup]", "", []string{"capability", "mcp__demo__lookup"}},
		{"mcp wildcard", "tools: [capability, mcp__demo__lookup*]", "exact", nil},
		{"mcp proxy missing", "tools: [mcp__demo__lookup]", "capability", nil},
		{"empty skill", "skills: ['']", "skill names", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, err := parse("---\nname: reviewer\n"+tc.fields+"\n---\nReview code.", knownTool)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("got %v, want %s", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(p.Tools, tc.tools) {
				t.Fatalf("tools=%#v, want %#v", p.Tools, tc.tools)
			}
		})
	}
}

func TestAgentMDRejectsEscapingPaths(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "plugin")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	content := []byte("---\nname: reviewer\ntools: [Read]\n---\nReview.")
	outside := filepath.Join(parent, "outside.md")
	inside := filepath.Join(root, "review.md")
	for _, path := range []string{outside, inside} {
		if err := os.WriteFile(path, content, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Load(root, inside, knownTool); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{outside, filepath.Join(root, "..", "outside.md")} {
		if _, err := Load(root, path, knownTool); err == nil {
			t.Fatal("accepted escaping path")
		}
	}
	link := filepath.Join(root, "linked.md")
	if err := os.Symlink(outside, link); err != nil {
		t.Skip(err)
	}
	if _, err := Load(root, link, knownTool); err == nil {
		t.Fatal("accepted escaping symlink")
	}
}

func TestBuiltinsAreIndependentAndReadOnly(t *testing.T) {
	p := Builtins()
	for _, tool := range []string{"shell", "process", "edit", "write"} {
		if slices.Contains(p[1].Tools, tool) {
			t.Fatal("explore gained write tools")
		}
	}
	p[0].Tools[0] = "bad"
	if Builtins()[0].Tools[0] != "read" {
		t.Fatal("shared builtin slice")
	}
}
