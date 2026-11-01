package policy

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func diagnosticHook(name, version string) Hook {
	return Hook{Name: name, Point: PreModel, BehaviorVersion: version, On: func(State) *Result { return nil }}
}

func TestConfigurationIsStableOrderedAndExplicit(t *testing.T) {
	a, b := New(), New()
	for _, p := range []*Policy{a, b} {
		p.Register(diagnosticHook("first", "1"))
		p.Register(diagnosticHook("second", "1"))
	}
	original := a.Configuration()
	if original != b.Configuration() || original.Incomplete || len(original.Hash) != 64 {
		t.Fatal("same declarations differ or are incomplete")
	}
	a.ResetRun()
	a.SetSessionID("private-session")
	if got := a.Configuration(); got != original {
		t.Fatal("run/session state changed configuration")
	}
	a.Register(diagnosticHook("first", "1"))
	replaced := a.Configuration()
	if replaced.Hash != original.Hash || replaced.Revision <= original.Revision {
		t.Fatal("same declaration replacement lost stable hash/local revision")
	}
	a.Register(diagnosticHook("first", "2"))
	if a.Configuration().Hash == original.Hash {
		t.Fatal("behavior version change was invisible")
	}
	reversed := New()
	reversed.Register(diagnosticHook("second", "1"))
	reversed.Register(diagnosticHook("first", "1"))
	if reversed.Configuration().Hash == original.Hash {
		t.Fatal("hook execution order ignored")
	}
	b.UnregisterPrefix("first")
	if b.Configuration().Revision <= original.Revision || b.Configuration().Hash == original.Hash {
		t.Fatal("removal not observed")
	}
	revision := b.Configuration().Revision
	b.UnregisterPrefix("missing")
	if b.Configuration().Revision != revision {
		t.Fatal("no-op removal advanced revision")
	}
}

func TestConfigurationDoesNotExecuteOrExposeHookState(t *testing.T) {
	p := New()
	secret := "credential-sentinel"
	invoked := false
	p.Register(Hook{Name: "custom-" + secret, Point: PreModel, On: func(State) *Result { invoked = true; return nil }})
	configuration := p.Configuration()
	if invoked || !configuration.Incomplete {
		t.Fatal("custom callback executed or claimed complete")
	}
	raw, err := json.Marshal(configuration)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), secret) {
		t.Fatal("declaration values leaked into diagnostic")
	}
	var absent *Policy
	if absent.Configuration().Incomplete {
		t.Fatal("empty policy has unknown callbacks")
	}
}

func TestConfigurationPreservesEffectiveTimeoutPrecision(t *testing.T) {
	a, b := New(), New()
	first, second := diagnosticHook("timeout", "1"), diagnosticHook("timeout", "1")
	first.Timeout = time.Millisecond
	second.Timeout = 1900 * time.Microsecond
	a.Register(first)
	b.Register(second)
	if a.Configuration().Hash == b.Configuration().Hash {
		t.Fatal("distinct effective timeouts have the same configuration hash")
	}
}
