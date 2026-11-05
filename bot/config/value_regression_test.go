package config

import (
	"reflect"
	"testing"
)

func TestMCPArgumentsAndEnvironmentPreserveExactValues(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := Default.Clone()
	want := MCPServerConfig{Command: "server", Args: []string{" value ", "\nscript\n", ""}, Env: map[string]string{"TOKEN": " secret ", "EMPTY": ""}}
	cfg.MCPServers = map[string]MCPServerConfig{"server": want}
	if err := Save(cfg); err != nil {
		t.Fatal(err)
	}
	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	server := got.MCPServers["server"]
	if !reflect.DeepEqual(server.Args, want.Args) || !reflect.DeepEqual(server.Env, want.Env) {
		t.Fatalf("MCP payload changed: args=%q env=%q", server.Args, server.Env)
	}
}

func TestCloneJevValuesAreIndependent(t *testing.T) {
	enabled, threshold := true, 0.2
	original := Config{Jev: &JevConfig{Enabled: &enabled, KeepThreshold: &threshold}}
	copy := original.Clone()
	*copy.Jev.Enabled = false
	*copy.Jev.KeepThreshold = 0.9
	if !*original.Jev.Enabled || *original.Jev.KeepThreshold != 0.2 {
		t.Fatal("mutating cloned Jev values changed the original configuration")
	}
	copy = (Config{Jev: &JevConfig{}}).Clone()
	if copy.Jev.Enabled != nil || copy.Jev.KeepThreshold != nil || (Config{}).Clone().Jev != nil {
		t.Fatal("cloning changed nil semantics")
	}
}
