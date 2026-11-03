package permission

import (
	"encoding/json"
	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/workspace"
	"os"
	"path/filepath"
	"testing"
)

func TestProjectCannotSupplyTrustedApprovals(t *testing.T) {
	project := t.TempDir()
	store := NewStore(project)
	legacy := filepath.Join(project, ".nekocode", "permissions.json")
	if err := os.MkdirAll(filepath.Dir(legacy), 0700); err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(permissionFile{Version: 1, Projects: map[string]permissionProject{project: {Grants: []Grant{{Effect: "allow", Tool: "shell", Capabilities: []string{core.CapProcessHost}, Workspace: project}}, Rules: []Rule{{Tool: "shell", Specifier: "*", Effect: EffectAllow}}, Workspaces: []WorkspaceRoot{{Path: project, Access: "read-write"}}}}})
	if err := os.WriteFile(legacy, payload, 0600); err != nil {
		t.Fatal(err)
	}
	req := core.PermissionRequest{Capabilities: []string{core.CapProcessHost}}
	if _, ok := store.Match("shell", req); ok {
		t.Fatal("trusted project-controlled capability")
	}
	if len(store.RememberedRules(project)) != 0 || len(store.WorkspaceRoots()) != 0 {
		t.Fatal("trusted project-controlled rules or roots")
	}
	if err := store.Allow("shell", req); err != nil {
		t.Fatal(err)
	}
	if _, ok := NewStore(project).Match("shell", req); !ok {
		t.Fatal("trusted grant did not survive store recreation")
	}
	after, err := os.ReadFile(legacy)
	if err != nil || string(after) != string(payload) {
		t.Fatal("legacy file was migrated or modified")
	}
	m := workspace.New(filepath.Dir(store.Path()), nil)
	if _, _, ok, err := m.CheckWrite(store.Path()); err == nil || ok {
		t.Fatal("trusted store writable through workspace")
	}
}
