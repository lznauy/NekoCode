package runner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nekocode/bot/extension/tool/runtime/core"
	"nekocode/bot/extension/tool/runtime/permission"
	"nekocode/protocol"
)

func TestWorkspaceApprovalSaveFailureDoesNotGrantAccess(t *testing.T) {
	e := newPermTestExecutor(t)
	primary, extra := t.TempDir(), t.TempDir()
	e.SetProjectStore(primary)
	e.workspace.Configure(primary, nil)
	dir := filepath.Dir(e.permStore.Path())
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "permissions.json"), []byte("broken JSON"), 0600); err != nil {
		t.Fatal(err)
	}
	call := core.ToolCallItem{Name: "write", Args: map[string]any{"path": filepath.Join(extra, "output.txt")}}
	_, message, ok := e.ensureWorkspaceAccess(call, func(protocol.ConfirmRequest) protocol.ConfirmReply { return protocol.AllowRemembered() })
	if ok || !strings.Contains(message, "cannot save workspace approval") {
		t.Fatalf("workspace save failure hidden: ok=%t message=%s", ok, message)
	}
	if _, _, allowed, err := e.workspace.CheckWrite(call.Args["path"].(string)); err != nil || allowed {
		t.Fatalf("failed approval left a session grant: allowed=%t err=%v", allowed, err)
	}
	if _, message, ok := e.ensureWorkspaceAccess(call, func(protocol.ConfirmRequest) protocol.ConfirmReply { return protocol.AllowOnce() }); !ok {
		t.Fatalf("one-time workspace approval should work: %s", message)
	}
}

func TestWorkspaceApprovalRejectsChangedContext(t *testing.T) {
	for _, change := range []string{"project", "session", "session_aba", "configuration", "symlink"} {
		t.Run(change, func(t *testing.T) {
			e := newPermTestExecutor(t)
			primary, extra, other := t.TempDir(), t.TempDir(), t.TempDir()
			e.SetProjectStore(primary)
			e.workspace.Configure(primary, nil)
			e.workspace.SetSession("A")
			call := core.ToolCallItem{Name: "write", Args: map[string]any{"path": filepath.Join(extra, "output.txt")}}
			_, message, ok := e.ensureWorkspaceAccess(call, func(protocol.ConfirmRequest) protocol.ConfirmReply {
				switch change {
				case "project":
					e.SetProjectStore(other)
				case "session":
					e.workspace.SetSession("B")
				case "session_aba":
					e.workspace.SetSession("B")
					e.workspace.SetSession("A")
				case "configuration":
					e.workspace.Configure(other, nil)
				case "symlink":
					if err := os.Rename(extra, extra+"-moved"); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(other, extra); err != nil {
						t.Skipf("symlinks unavailable: %v", err)
					}
				}
				return protocol.AllowRemembered()
			})
			if ok {
				t.Fatalf("approval survived changed %s: %s", change, message)
			}
			for _, root := range []string{primary, other} {
				if saved := permission.NewStore(root).WorkspaceRoots(); len(saved) != 0 {
					t.Fatalf("stale approval persisted: %+v", saved)
				}
			}
			if len(e.workspace.Snapshot()) != 1 {
				t.Fatalf("stale approval left an extra session root: %+v", e.workspace.Snapshot())
			}
		})
	}
}
