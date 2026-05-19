package tools

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileCache(t *testing.T) {
	td := t.TempDir()
	p := filepath.Join(td, "test.txt")
	os.WriteFile(p, []byte("hello\nworld\nfoo\nbar\nbaz\n"), 0644)

	c := NewFileStateCache()

	// Miss on empty cache.
	if _, hit := c.Get(p, 1, 3); hit {
		t.Error("expected miss on empty cache")
	}

	// Put lines + Get.
	lines := []string{"hello", "world", "foo", "bar", "baz"}
	c.Put(p, lines, 1, 3)
	hint, hit := c.Get(p, 1, 3)
	if !hit || hint == "" {
		t.Error("expected cache hit for covered range 1-3")
	}

	// Subset coverage: range 2-3 is within 1-3.
	hint, hit = c.Get(p, 2, 3)
	if !hit || hint == "" {
		t.Error("expected cache hit for subset range 2-3")
	}

	// Partial overlap → miss.
	if _, hit := c.Get(p, 2, 5); hit {
		t.Error("expected miss for partially covered range 2-5")
	}

	// Add new range and verify merge.
	c.Put(p, lines, 4, 5)
	hint, hit = c.Get(p, 2, 4)
	if !hit || hint == "" {
		t.Error("expected hit after merging ranges 1-3 and 4-5")
	}

	// Lines() returns full content.
	if cached, ok := c.Lines(p); !ok || len(cached) != 5 {
		t.Error("expected Lines() to return full cached content")
	}

	// Invalidate.
	c.Invalidate(p)
	if _, hit := c.Get(p, 1, 2); hit {
		t.Error("expected miss after invalidate")
	}
}

func TestFileCacheMerge(t *testing.T) {
	td := t.TempDir()
	p1 := filepath.Join(td, "a.txt")
	p2 := filepath.Join(td, "b.txt")
	os.WriteFile(p1, []byte("aaa\n"), 0644)
	os.WriteFile(p2, []byte("bbb\n"), 0644)

	main := NewFileStateCache()
	sub := NewFileStateCache()

	main.Put(p1, []string{"aaa"}, 1, 1)
	sub.Put(p2, []string{"bbb"}, 1, 1)
	main.Merge(sub)

	if _, hit := main.Get(p2, 1, 1); !hit {
		t.Error("expected hit after merge")
	}
}

func TestFileCacheEviction(t *testing.T) {
	td := t.TempDir()
	c := NewFileStateCache()

	for i := range 110 {
		p := filepath.Join(td, string(rune('a'+i%26))+string(rune('0'+i/26)))
		os.WriteFile(p, []byte("x\n"), 0644)
		c.Put(p, []string{"x"}, 1, 1)
	}
	if len(c.entries) > maxCacheEntries {
		t.Errorf("expected <= %d entries, got %d", maxCacheEntries, len(c.entries))
	}
}

func TestMergeRanges(t *testing.T) {
	var r []lineRange

	r = mergeRanges(r, lineRange{1, 3})  // [1-3]
	r = mergeRanges(r, lineRange{5, 7})  // [1-3, 5-7]
	r = mergeRanges(r, lineRange{3, 5})  // should merge to [1-7]

	if len(r) != 1 || r[0].Start != 1 || r[0].End != 7 {
		t.Errorf("expected merged range [1-7], got %+v", r)
	}
}
