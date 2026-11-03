package permission

import (
	"nekocode/bot/extension/tool/runtime/core"
	"os"
	"path/filepath"
	"testing"
)

func TestStoreDecisionReportsUnreadablePolicy(t *testing.T) {
	store := NewStore(t.TempDir())
	if err := os.MkdirAll(filepath.Dir(store.Path()), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.Path(), []byte("invalid JSON"), 0600); err != nil {
		t.Fatal(err)
	}
	d := store.Decide([]string{"shell", "bash"}, core.PermissionRequest{Capabilities: []string{core.CapProcessHost}})
	if d.Err == nil || d.Allow || d.Deny {
		t.Fatalf("corrupt policy must remain distinguishable from no match: %+v", d)
	}
}
func TestStoreDecisionDenyWinsAcrossAliases(t *testing.T) {
	root := t.TempDir()
	store := NewStore(root)
	req := core.PermissionRequest{Capabilities: []string{core.CapProcessHost}}
	if err := store.mutate(func(f *permissionFile) bool {
		f.Projects[root] = permissionProject{Grants: []Grant{
			{Effect: "allow", Tool: "shell", Capabilities: req.Capabilities},
			{Effect: "deny", Tool: "bash", Capabilities: req.Capabilities},
		}}
		return true
	}); err != nil {
		t.Fatal(err)
	}
	for _, aliases := range [][]string{{"shell", "bash"}, {"bash", "shell"}} {
		d := store.Decide(aliases, req)
		if d.Err != nil || !d.Deny || d.Allow || d.Grant.Tool != "bash" {
			t.Fatalf("alias order changed deny precedence: %+v", d)
		}
	}
}

func TestStoreDecisionMissingPolicyAndAliasAllow(t *testing.T) {
	store := NewStore(t.TempDir())
	req := core.PermissionRequest{Capabilities: []string{core.CapProcessHost}}
	if d := store.Decide([]string{"shell", "bash"}, req); d.Err != nil || d.Allow || d.Deny {
		t.Fatalf("missing policy must remain an unmatched request: %+v", d)
	}
	if err := store.Allow("bash", req); err != nil {
		t.Fatal(err)
	}
	if d := store.Decide([]string{"shell", "bash"}, req); d.Err != nil || !d.Allow || d.Deny || d.Grant.Tool != "shell" {
		t.Fatalf("legacy alias grant not recognized: %+v", d)
	}
	if err := os.Remove(store.Path()); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(store.Path(), 0700); err != nil {
		t.Fatal(err)
	}
	if d := store.Decide([]string{"shell"}, req); d.Err == nil || d.Allow || d.Deny {
		t.Fatalf("filesystem read error disappeared: %+v", d)
	}
}
