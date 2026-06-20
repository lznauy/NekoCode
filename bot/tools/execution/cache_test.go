package execution

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

	if _, hit := c.Lines(p); hit {
		t.Error("expected miss on empty cache")
	}

	lines := []string{"hello", "world", "foo", "bar", "baz"}
	c.Put(p, lines, 1, 3)
	if cached, ok := c.Lines(p); !ok || len(cached) != 5 {
		t.Error("expected Lines() to return full cached content after Put")
	}

	if cached, ok := c.Lines(p); !ok || len(cached) != 5 {
		t.Error("expected Lines() to return full cached content")
	}

	c.Invalidate(p)
	if _, hit := c.Lines(p); hit {
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

	if _, hit := main.Lines(p2); !hit {
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

	r = mergeRanges(r, lineRange{1, 3})
	r = mergeRanges(r, lineRange{5, 7})
	r = mergeRanges(r, lineRange{3, 5})

	if len(r) != 1 || r[0].Start != 1 || r[0].End != 7 {
		t.Errorf("expected merged range [1-7], got %+v", r)
	}
}
