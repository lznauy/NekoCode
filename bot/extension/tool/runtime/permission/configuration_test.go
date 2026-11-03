package permission

import (
	"os"
	"testing"
	"time"

	"nekocode/bot/extension/tool/runtime/core"
)

func TestRulesHashTracksEffectiveDeclarationsNotTimestamps(t *testing.T) {
	e := NewEngine(DefaultMatchers())
	rule := Rule{Tool: "read", Specifier: "/src/**", Effect: EffectAllow, Source: "declared", CreatedAt: time.Now()}
	e.SetRules([]Rule{rule})
	first := e.RulesHash("/repo", "/home/user")
	rule.CreatedAt = time.Now().Add(time.Hour)
	e.SetRules([]Rule{rule})
	if e.RulesHash("/repo", "/home/user") != first {
		t.Fatal("timestamp changed rule declaration")
	}
	if e.RulesHash("/other", "/home/user") == first || e.RulesHash("/repo", "/other") == first {
		t.Fatal("path anchor change invisible")
	}
	rule.Specifier = "/other/**"
	e.SetRules([]Rule{rule})
	if e.RulesHash("/repo", "/home/user") == first {
		t.Fatal("rule change invisible")
	}
	e.SetSandboxRules([]SandboxRule{{Rule: rule, Profile: SandboxProfile{OutboundDomains: nil}}})
	unrestricted := e.RulesHash("/repo", "/home/user")
	e.SetSandboxRules([]SandboxRule{{Rule: rule, Profile: SandboxProfile{OutboundDomains: []string{}}}})
	if e.RulesHash("/repo", "/home/user") == unrestricted {
		t.Fatal("nil and empty network ceilings conflated")
	}
}

func TestGrantHashObservesOtherStoreAndLoadFailure(t *testing.T) {
	root := t.TempDir()
	a, b := NewStore(root), NewStore(root)
	first, err := a.GrantsHash()
	if err != nil {
		t.Fatal(err)
	}
	req := core.PermissionRequest{Capabilities: []string{core.CapNetOutbound}, Reason: "private-reason-sentinel"}
	if err := b.Allow("shell", req); err != nil {
		t.Fatal(err)
	}
	next, err := a.GrantsHash()
	if err != nil || next == first {
		t.Fatalf("other store mutation invisible: %v", err)
	}
	if _, ok := a.Match("shell", req); !ok {
		t.Fatal("observation changed authority")
	}
	if err := os.WriteFile(a.Path(), []byte("malformed-secret-sentinel"), 0600); err != nil {
		t.Fatal(err)
	}
	if hash, err := a.GrantsHash(); err == nil || hash != "" {
		t.Fatal("invalid store presented as known configuration")
	}
}
