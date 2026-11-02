package execution

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"testing"
)

func TestFileCache(t *testing.T) {
	td := t.TempDir()
	p := filepath.Join(td, "test.txt")
	os.WriteFile(p, []byte("hello\nworld\nfoo\nbar\nbaz\n"), 0644)

	c := NewFileStateCache()
	version := sha256.Sum256([]byte("hello\nworld\nfoo\nbar\nbaz\n"))

	if _, hit := c.Lines(p, version); hit {
		t.Error("expected miss on empty cache")
	}

	lines := []string{"hello", "world", "foo", "bar", "baz"}
	c.Put(p, lines, version)
	if cached, ok := c.Lines(p, version); !ok || len(cached) != 5 {
		t.Error("expected Lines() to return full cached content after Put")
	}

	if cached, ok := c.Lines(p, version); !ok || len(cached) != 5 {
		t.Error("expected Lines() to return full cached content")
	}

	c.Invalidate(p)
	if _, hit := c.Lines(p, version); hit {
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

	main.Put(p1, []string{"aaa"}, sha256.Sum256([]byte("aaa\n")))
	sub.Put(p2, []string{"bbb"}, sha256.Sum256([]byte("bbb\n")))
	main.Merge(sub)

	if _, hit := main.Lines(p2, sha256.Sum256([]byte("bbb\n"))); !hit {
		t.Error("expected hit after merge")
	}
}

func TestFileCacheEviction(t *testing.T) {
	td := t.TempDir()
	c := NewFileStateCache()

	for i := range 110 {
		p := filepath.Join(td, string(rune('a'+i%26))+string(rune('0'+i/26)))
		os.WriteFile(p, []byte("x\n"), 0644)
		c.Put(p, []string{"x"}, sha256.Sum256([]byte("x\n")))
	}
	if len(c.entries) > maxCacheEntries {
		t.Errorf("expected <= %d entries, got %d", maxCacheEntries, len(c.entries))
	}
}

func TestFileCacheVersionReplacementKeepsOrderBounded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(path, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	cache := NewFileStateCache()
	for i := 0; i < 1000; i++ {
		version := [32]byte{byte(i), byte(i >> 8)}
		cache.Put(path, []string{"x"}, version)
	}
	if len(cache.order) != 1 {
		t.Fatalf("replacement grew eviction queue: %d", len(cache.order))
	}
	if lines, ok := cache.Lines(path, [32]byte{}); ok || lines != nil {
		t.Fatal("version miss returned stale lines")
	}
}
