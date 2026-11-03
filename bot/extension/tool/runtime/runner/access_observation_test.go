package runner

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/permission"
	"nekocode/bot/extension/tool/runtime/workspace"
)

func TestAccessObservationTracksActorModeRulesAndGrants(t *testing.T) {
	parent, child := newTestExecutor(nil), newTestExecutor(nil)
	root := t.TempDir()
	parent.workspace.Configure(root, nil)
	parent.SetProjectStore(root)
	parent.SetPermissionPolicy(permission.PermissionsDecl{Deny: []string{"read(secret-sentinel/**)"}}, root, root)
	parent.ConfigureChildPermissions(child)
	before := child.AccessObservation()
	if before.Mode != "manual" || before.RulesHash == "" || before.StoreUnavailable {
		t.Fatalf("missing declaration: %+v", before)
	}
	parent.SetFullAccess(true)
	after := child.AccessObservation()
	if after.Mode != "full" || after.RulesHash != before.RulesHash {
		t.Fatal("live parent mode confused with rule declarations")
	}
	child.SetPlanMode(true)
	if !child.AccessObservation().PlanMode {
		t.Fatal("plan transition invisible")
	}
	if err := parent.permStore.Allow("shell", core.PermissionRequest{Capabilities: []string{core.CapNetOutbound}}); err != nil {
		t.Fatal(err)
	}
	granted := child.AccessObservation()
	if granted.GrantsHash == before.GrantsHash {
		t.Fatal("shared project grant invisible")
	}
	if _, err := child.workspace.AddSessionRoot(t.TempDir(), workspace.AccessReadOnly); err != nil {
		t.Fatal(err)
	}
	if child.AccessObservation().WorkspaceHash == before.WorkspaceHash {
		t.Fatal("child root change invisible")
	}
	raw, err := json.Marshal(granted)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), root) || strings.Contains(string(raw), "secret-sentinel") {
		t.Fatal("rule or path leaked into observation")
	}
	if err := os.WriteFile(parent.permStore.Path(), []byte("invalid-secret-sentinel"), 0600); err != nil {
		t.Fatal(err)
	}
	failed := child.AccessObservation()
	if !failed.StoreUnavailable || failed.GrantsHash != "" || child.permissionMode() != permission.ModeFull {
		t.Fatal("diagnostic failure changed mode or concealed unknown store")
	}
}
