package indexer

import (
	"fmt"
	graphpkg "nekocode/bot/extension/tool/builtin/index/core/internal/graph"
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkResolveExternalImports(b *testing.B) {
	g := graphpkg.NewGraph()
	for j := 0; j < 10000; j++ {
		g.AddNode(&graphpkg.Node{Name: fmt.Sprintf("F%d", j), PkgPath: fmt.Sprintf("local%d", j/10)})
	}
	for j := 0; j < 1000; j++ {
		g.AddEdge(&graphpkg.Edge{FromID: 1, Kind: graphpkg.EdgeImports, ImportPath: fmt.Sprintf("external/missing%d", j)})
	}
	b.ReportAllocs()
	b.ResetTimer()
	for j := 0; j < b.N; j++ {
		resolveReferences(g)
	}
}
func BenchmarkStartupSingleChangedFile(b *testing.B) {
	dir, idx := benchmarkProject(b)
	if _, err := idx.IndexAll(dir); err != nil {
		b.Fatal(err)
	}
	path := filepath.Join(dir, "file0.go")
	original, err := os.ReadFile(path)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for j := 0; j < b.N; j++ {
		b.StopTimer()
		content := append(append([]byte(nil), original...), []byte(fmt.Sprintf("\n// edit %d", j))...)
		if err := os.WriteFile(path, content, 0600); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
		if _, err := idx.LoadOrBuild(dir); err != nil {
			b.Fatal(err)
		}
	}
}
