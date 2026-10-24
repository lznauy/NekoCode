package parser

import (
	"fmt"
	"strings"
	"sync"
	"testing"
)

func TestParserReuseKeepsFileResultsIndependentAndCloses(t *testing.T) {
	p := NewParser()
	defer p.Close()
	for j := 0; j < 20; j++ {
		name := fmt.Sprintf("Function%d", j)
		nodes, _ := p.ParseFile(fmt.Sprintf("file%d.go", j), []byte("package sample\nfunc "+name+"() {}"))
		found := false
		for _, n := range nodes {
			if n.Name == name {
				found = true
			}
			if n.Name == "Function0" && j > 0 {
				t.Fatal("query cursor returned stale captures")
			}
		}
		if !found {
			t.Fatalf("missing %s", name)
		}
	}
	p.Close()
	p.Close()
	if nodes, edges := p.ParseFile("closed.go", []byte("package sample\nfunc AfterClose() {}")); len(nodes) != 0 || len(edges) != 0 {
		t.Fatal("closed parser produced results")
	}
}

func TestParserConcurrentReuse(t *testing.T) {
	p := NewParser()
	defer p.Close()
	var wg sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			name := fmt.Sprintf("Worker%d", worker)
			for j := 0; j < 10; j++ {
				nodes, _ := p.ParseFile(name+".go", []byte("package sample\nfunc "+name+"() {}"))
				found := false
				for _, n := range nodes {
					if n.Name == name {
						found = true
					}
				}
				if !found {
					t.Errorf("missing %s", name)
					return
				}
			}
		}(worker)
	}
	wg.Wait()
}

func TestParserCachedQueriesAcrossLanguages(t *testing.T) {
	p := NewParser()
	defer p.Close()
	fixtures := []struct {
		ext, source string
	}{
		{".go", "package sample\nfunc %s() { Target() }"},
		{".js", "function %s() { Target(); }"},
		{".jsx", "function %s() { Target(); }"},
		{".ts", "function %s(): void { Target(); }"},
		{".tsx", "function %s(): void { Target(); }"},
		{".py", "def %s():\n    Target()\n"},
		{".rs", "fn %s() { Target(); }"},
	}
	for iteration := 0; iteration < 3; iteration++ {
		for j, fixture := range fixtures {
			name := fmt.Sprintf("Function%d_%d", iteration, j)
			nodes, edges := p.ParseFile("sample"+fixture.ext, []byte(fmt.Sprintf(fixture.source, name)))
			if len(nodes) != 1 || nodes[0].Name != name || nodes[0].Kind != KindFunc {
				t.Fatalf("%s iteration %d: incorrect cached definition: %+v", fixture.ext, iteration, nodes)
			}
			if len(edges) != 1 || edges[0].CalleeName != "Target" || edges[0].FromID != -1 {
				t.Fatalf("%s iteration %d: incorrect cached call: %+v", fixture.ext, iteration, edges)
			}
		}
	}
}

func BenchmarkParseSmallFile(b *testing.B) {
	p := NewParser()
	defer p.Close()
	var source strings.Builder
	source.WriteString("package sample\n")
	for j := 0; j < 12; j++ {
		fmt.Fprintf(&source, "func F%d() { F%d() }\n", j, (j+1)%12)
	}
	code := []byte(source.String())
	p.ParseFile("a.go", code)
	b.ReportAllocs()
	b.ResetTimer()
	for j := 0; j < b.N; j++ {
		p.ParseFile("a.go", code)
	}
}
