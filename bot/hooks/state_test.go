package hooks

import "testing"

func TestSnapshotSetAccumulatesPatch(t *testing.T) {
	snap := &Snapshot{
		Store:   make(map[string]int64),
		strVals: make(map[string]string),
	}

	snap.set("counter:test", 3)
	snap.setStr("value:test", "ok")

	if snap.patch.Ints["counter:test"] != 3 {
		t.Fatalf("int patch = %d, want 3", snap.patch.Ints["counter:test"])
	}
	if snap.patch.Strings["value:test"] != "ok" {
		t.Fatalf("string patch = %q, want ok", snap.patch.Strings["value:test"])
	}
}
