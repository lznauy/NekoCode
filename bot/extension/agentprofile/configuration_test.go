package agentprofile

import "testing"

func TestConfigurationHashNormalizesOnlySetAndDefaults(t *testing.T) {
	original := Profile{Name: "coder", Tools: []string{"write", "read", "read"}, Skills: []string{"first", "second"}}
	same := Clone(original)
	same.Tools = []string{"read", "write"}
	same.MaxSteps = DefaultMaxSteps
	if original.ConfigurationHash() != same.ConfigurationHash() {
		t.Fatal("tool set/defaults changed hash")
	}
	if original.Tools[0] != "write" {
		t.Fatal("fingerprinting mutated profile")
	}
	for _, change := range []func(*Profile){
		func(p *Profile) { p.MaxSteps = 1 },
		func(p *Profile) { p.Tools = []string{"read"} },
		func(p *Profile) { p.Skills = []string{"second", "first"} },
		func(p *Profile) { p.Model = "other" },
		func(p *Profile) { p.Isolation = "worktree" },
	} {
		different := Clone(original)
		change(&different)
		if different.ConfigurationHash() == original.ConfigurationHash() {
			t.Fatal("execution declaration change invisible")
		}
	}
	same = Clone(original)
	same.Description = "UI metadata"
	same.SystemPrompt = "already covered by actual system prefix"
	if original.ConfigurationHash() != same.ConfigurationHash() {
		t.Fatal("duplicated existing prompt/UI diagnostics")
	}
	if len(original.ConfigurationHash()) != 67 {
		t.Fatal("missing versioned full SHA256")
	}
}
