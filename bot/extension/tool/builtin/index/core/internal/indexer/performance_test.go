package indexer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	graphpkg "nekocode/bot/extension/tool/builtin/index/core/internal/graph"
)

func benchmarkProject(b *testing.B) (string, *Indexer) {
	b.Helper()
	dir := b.TempDir()
	for file := 0; file < 40; file++ {
		var code strings.Builder
		code.WriteString("package sample\n")
		for fn := 0; fn < 12; fn++ {
			fmt.Fprintf(&code, "func F%d_%d() { F%d_%d() }\n", file, fn, file, (fn+1)%12)
		}
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("file%d.go", file)), []byte(code.String()), 0600); err != nil {
			b.Fatal(err)
		}
	}
	idx, err := NewIndexer(filepath.Join(b.TempDir(), "index.db"))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { idx.Close() })
	return dir, idx
}

func BenchmarkIndexAll(b *testing.B) {
	dir, idx := benchmarkProject(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		g, err := idx.IndexAll(dir)
		if err != nil {
			b.Fatal(err)
		}
		if g.FileCount() != 40 {
			b.Fatal("incomplete graph")
		}
	}
}

func BenchmarkUpdateFiles(b *testing.B) {
	dir, idx := benchmarkProject(b)
	g, err := idx.IndexAll(dir)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		paths := make([]string, 20)
		contents := make([][]byte, 20)
		for j := range paths {
			paths[j] = filepath.Join(dir, fmt.Sprintf("file%d.go", j))
			contents[j] = []byte(fmt.Sprintf("package sample\nfunc Updated%d() { F39_0() }\n// iteration %d", j, i))
		}
		b.StartTimer()
		if err := applyBenchmarkBatch(idx, g, dir, paths, contents); err != nil {
			b.Fatal(err)
		}
	}
}

func applyBenchmarkBatch(idx *Indexer, g *graphpkg.Graph, dir string, paths []string, contents [][]byte) error {
	changes := make([]FileChange, len(paths))
	for j, path := range paths {
		changes[j] = FileChange{Path: path, Content: contents[j]}
	}
	return idx.ApplyChanges(g, dir, changes)
}
