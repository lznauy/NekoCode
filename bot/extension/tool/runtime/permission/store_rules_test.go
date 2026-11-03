package permission

import (
	"bytes"
	"nekocode/bot/extension/tool/runtime/core"
	"os"
	"testing"
)

func TestStoreRejectsPersistingOneTimeCapability(t *testing.T) {
	s := NewStore(t.TempDir())
	req := core.PermissionRequest{Scope: "once", Capabilities: []string{core.CapNetOutbound}}
	if err := s.Allow("shell", req); err == nil {
		t.Fatal("storage accepted a one-time grant")
	}
	if _, allowed := s.Match("shell", req); allowed {
		t.Fatal("one-time grant persisted")
	}
}

func TestRememberRulesValidatesWholeBatchAndDeduplicates(t *testing.T) {
	s := NewStore(t.TempDir())
	if err := s.RememberRule("", Rule{Tool: "shell", Specifier: "echo *", Effect: EffectAllow}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(s.projectPath())
	if err != nil {
		t.Fatal(err)
	}
	batch := []Rule{{Tool: "shell", Specifier: "curl *", Effect: EffectAllow}, {Tool: "shell", Specifier: "echo hello *", Effect: EffectAllow}}
	if err := s.RememberRules("", batch); err == nil {
		t.Fatal("invalid rule batch was accepted")
	}
	after, err := os.ReadFile(s.projectPath())
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("failed batch partially overwrote rules: %q, %v", after, err)
	}
	batch = []Rule{{Tool: "shell", Specifier: "curl *", Effect: EffectAllow}, {Tool: "shell", Specifier: "python *", Effect: EffectAllow}, {Tool: "bash", Specifier: "curl *", Effect: EffectAllow}}
	if err := s.RememberRules("", batch); err != nil {
		t.Fatal(err)
	}
	if err := s.RememberRules("", batch); err != nil {
		t.Fatal(err)
	}
	rules := NewStore(s.root).RememberedRules("")
	if len(rules) != 3 {
		t.Fatalf("duplicate or missing batch grants: %+v", rules)
	}
	for _, r := range rules {
		if r.Source != "remembered" {
			t.Fatalf("batch grant not marked remembered: %+v", r)
		}
	}
}
