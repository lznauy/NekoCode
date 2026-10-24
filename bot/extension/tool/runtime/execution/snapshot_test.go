package execution

import (
	"testing"

	"nekocode/bot/extension/tool/runtime/toolutil"
)

func TestSnapshotStore_Record(t *testing.T) {
	store := NewSnapshotStore()
	hash := store.Record("/test/file.go", "content")
	if len(hash) != 8 {
		t.Fatalf("expected 8-char hash, got %q", hash)
	}
	snap := store.ByHash("/test/file.go", hash)
	if snap == nil {
		t.Fatal("expected snapshot, got nil")
	}
	if snap.Hash != hash {
		t.Fatalf("hash mismatch: %q vs %q", snap.Hash, hash)
	}
}

func TestSnapshotStore_ReadFusion(t *testing.T) {
	store := NewSnapshotStore()
	hash1 := store.Record("/test/file.go", "content")
	hash2 := store.Record("/test/file.go", "content")
	if hash1 != hash2 {
		t.Fatalf("read fusion should reuse same hash: %q vs %q", hash1, hash2)
	}
}

func TestSnapshotStore_MultipleVersions(t *testing.T) {
	store := NewSnapshotStore()
	store.Record("/test/file.go", "v1")
	store.Record("/test/file.go", "v2")
	store.Record("/test/file.go", "v3")

	h3 := toolutil.ComputeFileHash("v3")
	snap := store.ByHash("/test/file.go", h3)
	if snap == nil || snap.Text != "v3" {
		t.Fatalf("expected latest to be v3, got %v", snap)
	}

	h1 := toolutil.ComputeFileHash("v1")
	old := store.ByHash("/test/file.go", h1)
	if old == nil || old.Text != "v1" {
		t.Fatalf("expected to find v1 by hash, got %v", old)
	}
}

func TestSnapshotStore_VersionLimit(t *testing.T) {
	store := NewSnapshotStore()
	store.maxPerPath = 2
	store.Record("/test/file.go", "v1")
	store.Record("/test/file.go", "v2")
	store.Record("/test/file.go", "v3")

	h1 := toolutil.ComputeFileHash("v1")
	if store.ByHash("/test/file.go", h1) != nil {
		t.Fatal("v1 should have been evicted")
	}
	h2 := toolutil.ComputeFileHash("v2")
	if store.ByHash("/test/file.go", h2) == nil {
		t.Fatal("v2 should still exist")
	}
}

func TestSnapshotStore_PathLimit(t *testing.T) {
	store := NewSnapshotStore()
	store.maxPaths = 2
	store.Record("/a.go", "a")
	store.Record("/b.go", "b")
	store.Record("/c.go", "c")

	ha := toolutil.ComputeFileHash("a")
	if store.ByHash("/a.go", ha) != nil {
		t.Fatal("/a.go should have been evicted")
	}
	hb := toolutil.ComputeFileHash("b")
	if store.ByHash("/b.go", hb) == nil {
		t.Fatal("/b.go should still exist")
	}
	hc := toolutil.ComputeFileHash("c")
	if store.ByHash("/c.go", hc) == nil {
		t.Fatal("/c.go should still exist")
	}
}

// GC scans the backing array beyond len, so evicted versions must be cleared.
func TestSnapshotStoreEvictionReleasesBackingReferences(t *testing.T) {
	store := NewSnapshotStore()
	store.maxPerPath = 2
	oldHash := store.Record("file", "old content")
	store.Record("file", "middle")
	store.Record("file", "new")
	versions := store.versions["file"]
	for _, snap := range versions[len(versions):cap(versions)] {
		if snap.Text != "" || snap.Path != "" || snap.Hash != "" {
			t.Fatalf("evicted snapshot retained: %+v", snap)
		}
	}
	if store.ByHash("file", oldHash) != nil {
		t.Fatal("evicted version remains visible")
	}
	if store.totalBytes != int64(len("middle")+len("new")) {
		t.Fatalf("bytes=%d", store.totalBytes)
	}
}
