package workspace

import "testing"

func TestConfigurationHashReflectsEffectiveScopeAndPrimary(t *testing.T) {
	root, first, second, extra := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	a := New(root, []Root{{Path: first, Access: AccessReadOnly}, {Path: second, Access: AccessReadWrite}})
	b := New(root, []Root{{Path: second, Access: AccessReadWrite}, {Path: first, Access: AccessReadOnly}})
	initial := a.ConfigurationHash()
	if initial != b.ConfigurationHash() {
		t.Fatal("unordered root declaration changed hash")
	}
	a.SetSession("one")
	if _, err := a.AddSessionRoot(extra, AccessReadOnly); err != nil {
		t.Fatal(err)
	}
	scoped := a.ConfigurationHash()
	if scoped == initial {
		t.Fatal("session grant invisible")
	}
	a.SetSession("two")
	if a.ConfigurationHash() != initial {
		t.Fatal("other session borrowed temporary grant")
	}
	a.SetSession("one")
	if a.ConfigurationHash() != scoped {
		t.Fatal("active authority not restored")
	}
	if _, _, allowed, err := a.CheckWrite(extra); err != nil || allowed {
		t.Fatal("diagnostic upgraded read-only root")
	}
	a.Configure(second, []Root{{Path: root, Access: AccessReadWrite}, {Path: first, Access: AccessReadOnly}})
	if a.ConfigurationHash() == initial {
		t.Fatal("different execution primary invisible")
	}
}
