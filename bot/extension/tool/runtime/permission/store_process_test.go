package permission

import (
	"nekocode/bot/extension/tool/runtime/core"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func waitStoreMarker(path string) error {
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			return nil
		} else if !os.IsNotExist(err) {
			return err
		}
		if time.Now().After(deadline) {
			return os.ErrDeadlineExceeded
		}
		time.Sleep(time.Millisecond)
	}
}

func TestStoreProcessLockPreservesConcurrentUpdates(t *testing.T) {
	if id := os.Getenv("NEKOCODE_STORE_LOCK_HELPER"); id != "" {
		if err := os.Setenv("HOME", os.Getenv("NEKOCODE_STORE_LOCK_HOME")); err != nil {
			t.Fatal(err)
		}
		if err := os.Setenv("USERPROFILE", os.Getenv("NEKOCODE_STORE_LOCK_HOME")); err != nil {
			t.Fatal(err)
		}
		root := os.Getenv("NEKOCODE_STORE_LOCK_PROJECT")
		if err := os.WriteFile(filepath.Join(root, id+"-ready"), nil, 0600); err != nil {
			t.Fatal(err)
		}
		err := NewStore(root).mutate(func(f *permissionFile) bool {
			if err := os.WriteFile(filepath.Join(root, id+"-entered"), nil, 0600); err != nil {
				panic(err)
			}
			if id == "a" {
				if err := waitStoreMarker(filepath.Join(root, "release")); err != nil {
					panic(err)
				}
			}
			p := f.Projects[root]
			p.Grants = append(p.Grants, Grant{Effect: "allow", Tool: id, Capabilities: []string{core.CapProcessHost}})
			f.Projects[root] = p
			return true
		})
		if err != nil {
			t.Fatal(err)
		}
		return
	}
	root := t.TempDir()
	start := func(id string) *exec.Cmd {
		t.Helper()
		cmd := exec.Command(os.Args[0], "-test.run=^TestStoreProcessLockPreservesConcurrentUpdates$")
		cmd.Env = append(os.Environ(), "NEKOCODE_STORE_LOCK_HELPER="+id, "NEKOCODE_STORE_LOCK_PROJECT="+root, "NEKOCODE_STORE_LOCK_HOME="+os.Getenv("HOME"))
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if cmd.ProcessState == nil {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
			}
		})
		return cmd
	}
	a := start("a")
	if err := waitStoreMarker(filepath.Join(root, "a-entered")); err != nil {
		t.Fatal(err)
	}
	b := start("b")
	if err := waitStoreMarker(filepath.Join(root, "b-ready")); err != nil {
		t.Fatal(err)
	}
	// A holds the transaction open; B must wait before reading or updating it.
	time.Sleep(100 * time.Millisecond)
	_, early := os.Stat(filepath.Join(root, "b-entered"))
	if err := os.WriteFile(filepath.Join(root, "release"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.Wait(); err != nil {
		t.Fatal(err)
	}
	if err := b.Wait(); err != nil {
		t.Fatal(err)
	}
	if early == nil {
		t.Error("second process entered a transaction while the first held it")
	}
	f, err := NewStore(root).load()
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Projects[root].Grants) != 2 {
		t.Fatalf("lost concurrent update: %+v", f.Projects[root].Grants)
	}
}
