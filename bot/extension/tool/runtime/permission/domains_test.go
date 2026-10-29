package permission

import (
	"encoding/json"
	"nekocode/bot/extension/tool/runtime/core"
	"slices"
	"strings"
	"testing"
)

func domainRequest(domains []string) core.PermissionRequest {
	r := core.PermissionRequest{Capabilities: []string{core.CapNetOutbound}, Details: map[string]any{}}
	if domains != nil {
		r.Details["outbound_domains"] = domains
	}
	return r
}
func TestDomainGrantsDoNotWiden(t *testing.T) {
	store := NewStore(t.TempDir())
	if err := store.Allow("shell", domainRequest([]string{"EXAMPLE.COM."})); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		domains []string
		allowed bool
	}{{[]string{"example.com"}, true}, {[]string{}, true}, {[]string{"evil.com"}, false}, {nil, false}} {
		_, ok := store.Match("shell", domainRequest(tc.domains))
		if ok != tc.allowed {
			t.Errorf("%v: allowed=%v", tc.domains, ok)
		}
	}
	if err := store.Allow("shell", domainRequest(nil)); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Match("shell", domainRequest(nil)); !ok {
		t.Fatal("legacy broad grant lost")
	}
	data, err := json.Marshal(Grant{OutboundDomains: []string{}})
	if err != nil || !strings.Contains(string(data), `"outbound_domains":[]`) {
		t.Fatalf("empty policy lost: %s %v", data, err)
	}
}
func TestSandboxDomainRulesIntersect(t *testing.T) {
	engine, err := NewEngineForWorkspace(PermissionsDecl{Sandbox: map[string]SandboxProfile{
		"shell": {OutboundDomains: []string{"example.com", "other.com"}}, "shell(curl *)": {Network: true, OutboundDomains: []string{"EXAMPLE.COM.", "third.com"}},
	}}, nil, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	profile, ok := engine.SandboxFor("shell", BuildCallInfo("shell", map[string]any{"command": "curl https://example.com"}, "", ""))
	if !ok || len(profile.OutboundDomains) != 1 || profile.OutboundDomains[0] != "example.com" {
		t.Fatalf("%+v", profile)
	}
	_, err = LoadSandboxRules(PermissionsDecl{Sandbox: map[string]SandboxProfile{"shell": {SandboxMode: "host", OutboundDomains: []string{}}}})
	if err == nil {
		t.Fatal("host+restricted allowed")
	}
}

func TestScopedGrantsAreNotBroadOnRollback(t *testing.T) {
	store := NewStore(t.TempDir())
	if err := store.Allow("shell", domainRequest([]string{"example.com"})); err != nil {
		t.Fatal(err)
	}
	grant, ok := store.Match("shell", domainRequest([]string{"example.com"}))
	if !ok {
		t.Fatal("scoped grant missing")
	}
	if containsAllCapabilities(grant.Capabilities, []string{core.CapNetOutbound}) {
		t.Fatal("old matcher would treat scoped grant as broad")
	}
}

func TestSandboxDomainCeilingSurvivesCompoundCommands(t *testing.T) {
	for _, broadDefault := range []bool{false, true} {
		rules := map[string]SandboxProfile{
			"shell(curl *)": {SandboxMode: "read-only", OutboundDomains: []string{"example.com"}},
		}
		if broadDefault {
			rules["shell"] = SandboxProfile{Network: true, SandboxMode: "workspace-write"}
		}
		loaded, err := LoadSandboxRules(PermissionsDecl{Sandbox: rules})
		if err != nil {
			t.Fatal(err)
		}
		engine := NewEngine(DefaultMatchers())
		engine.SetSandboxRules(loaded)
		for _, command := range []string{
			"curl https://evil.example; echo done",
			"echo start && curl https://evil.example",
			"curl https://evil.example || echo failed",
			"curl https://evil.example | cat",
			"curl https://evil.example & echo done",
			"curl https://evil.example\necho done",
		} {
			profile, matched := engine.SandboxFor("shell", BuildCallInfo("shell", map[string]any{"command": command}, "", ""))
			if !matched || !slices.Equal(profile.OutboundDomains, []string{"example.com"}) {
				t.Errorf("broad default=%v command=%q: domain ceiling lost: %+v matched=%v", broadDefault, command, profile, matched)
			}
			// Only the domain ceiling uses any-subcommand matching. Other
			// sandbox fields still require the original whole-call match.
			if broadDefault && profile.SandboxMode != "workspace-write" || !broadDefault && profile.SandboxMode != "" {
				t.Errorf("broad default=%v command=%q: sandbox mode changed: %+v", broadDefault, command, profile)
			}
		}
	}
}

func TestSandboxCompoundDomainCeilingsIntersect(t *testing.T) {
	for _, domains := range [][]string{{"example.com", "other.com"}, {"other.com"}, {}} {
		engine, err := NewEngineForWorkspace(PermissionsDecl{Sandbox: map[string]SandboxProfile{
			"shell":         {Network: true},
			"shell(curl *)": {OutboundDomains: []string{"example.com"}},
			"shell(wget *)": {OutboundDomains: domains},
		}}, nil, t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		profile, matched := engine.SandboxFor("shell", BuildCallInfo("shell", map[string]any{"command": "curl https://example.com; wget https://other.com"}, "", ""))
		want := []string{}
		if slices.Contains(domains, "example.com") {
			want = []string{"example.com"}
		}
		if !matched || profile.OutboundDomains == nil || !slices.Equal(profile.OutboundDomains, want) {
			t.Errorf("wget ceiling=%v: got %+v matched=%v; want non-nil domains=%v", domains, profile, matched, want)
		}
		profile, _ = engine.SandboxFor("shell", BuildCallInfo("shell", map[string]any{"command": "echo unrelated"}, "", ""))
		if profile.OutboundDomains != nil {
			t.Errorf("unrelated command inherited a domain ceiling: %+v", profile)
		}
	}
}

func TestSandboxExactCompoundRuleKeepsDomainCeiling(t *testing.T) {
	command := "curl https://example.com; echo done"
	engine, err := NewEngineForWorkspace(PermissionsDecl{Sandbox: map[string]SandboxProfile{
		"shell(" + command + ")": {Network: true, OutboundDomains: []string{"example.com"}},
	}}, nil, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	profile, matched := engine.SandboxFor("shell", BuildCallInfo("shell", map[string]any{"command": command}, "", ""))
	if !matched || !slices.Equal(profile.OutboundDomains, []string{"example.com"}) {
		t.Fatalf("exact compound rule lost domain ceiling: %+v matched=%v", profile, matched)
	}
}
