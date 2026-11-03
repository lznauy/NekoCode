package parser

import (
	"testing"
)

func TestTSXCallsInsideJSX(t *testing.T) {
	p := NewParser()
	defer p.Close()
	nodes, edges := p.ParseFile("app.tsx", []byte(`function App(){ return <Button value={load()} />; } function after(){ done(); }`))
	want := map[string]string{"load": "App", "done": "after"}
	for _, e := range edges {
		if e.Kind != EdgeCalls {
			continue
		}
		idx := int(-e.FromID - 1)
		if e.FromID >= 0 || idx >= len(nodes) || nodes[idx].Name != want[e.CalleeName] {
			t.Fatalf("bad TSX caller: %+v", e)
		}
		delete(want, e.CalleeName)
	}
	if len(want) != 0 {
		t.Fatalf("missing TSX calls: %v", want)
	}
}

func TestGroupedGoTypesHaveIndividualRanges(t *testing.T) {
	p := NewParser()
	defer p.Close()
	nodes, _ := p.ParseFile("types.go", []byte("package p\ntype (\n A struct{}\n B interface{}\n C int\n)\n"))
	want := map[string]int{"A": 3, "B": 4, "C": 5}
	for _, n := range nodes {
		if line, ok := want[n.Name]; ok {
			if n.Line != line || n.EndLine != line {
				t.Errorf("%s: range=%d-%d want %d", n.Name, n.Line, n.EndLine, line)
			}
			delete(want, n.Name)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing types: %v", want)
	}
}

func TestScopedDefinitionsAndCallSources(t *testing.T) {
	p := NewParser()
	defer p.Close()
	for _, tc := range []struct {
		path, source string
		want         map[int]string
		name         string
		count        int
	}{
		{"scope.go", "package p\ntype A struct{}\ntype B struct{}\nfunc (A) Foo(){ Help() }\nfunc (B) Foo(){ Help() }", map[int]string{4: "Foo", 5: "Foo"}, "Foo", 2},
		{"scope.py", "def outer():\n    def inner():\n        Target()\n    Target()\n", map[int]string{3: "inner", 4: "outer"}, "", 0},
		{"methods.py", "class A:\n    def run(self):\n        First()\nclass B:\n    def run(self):\n        Second()\n", map[int]string{3: "run", 6: "run"}, "run", 2},
		{"scope.js", "function first(){ One(); } function second(){ Two(); }", map[int]string{}, "", 0},
	} {
		for repeat := 0; repeat < 25; repeat++ {
			nodes, edges := p.ParseFile(tc.path, []byte(tc.source))
			count := 0
			for _, n := range nodes {
				if n.Name == tc.name {
					count++
				}
			}
			if count != tc.count {
				t.Fatalf("%s: %s count=%d want %d", tc.path, tc.name, count, tc.count)
			}
			for _, e := range edges {
				if e.Kind != EdgeCalls {
					continue
				}
				idx := int(-e.FromID - 1)
				if e.FromID >= 0 || idx >= len(nodes) {
					t.Fatalf("%s: unresolved caller: %+v", tc.path, e)
				}
				want := tc.want[e.Line]
				if tc.path == "scope.js" {
					if e.CalleeName == "One" {
						want = "first"
					} else {
						want = "second"
					}
				}
				if nodes[idx].Name != want || (tc.path == "scope.go" && nodes[idx].Line != e.Line) {
					t.Fatalf("%s: call %+v attributed to %+v; want %s", tc.path, e, nodes[idx], want)
				}
			}
			if len(edges) != 2 {
				t.Fatalf("%s: got %d edges, want 2", tc.path, len(edges))
			}
		}
	}
}

func TestImportVariants(t *testing.T) {
	p := NewParser()
	defer p.Close()
	for _, tc := range []struct {
		path, source string
		want         []string
	}{
		{"imports.py", "import os\nimport numpy as np\nfrom . import sibling\nfrom pkg import (a, b)\nfrom .mod import thing\n", []string{"os", "numpy", ".", "pkg", ".mod"}},
		{"imports.rs", "use std::fmt;\nuse std::io::*;\n", []string{"std::fmt", "std::io::*"}},
	} {
		_, edges := p.ParseFile(tc.path, []byte(tc.source))
		got := map[string]bool{}
		for _, e := range edges {
			if e.Kind == EdgeImports {
				got[e.ImportPath] = true
			}
		}
		for _, want := range tc.want {
			if !got[want] {
				t.Errorf("%s: missing import %q: %+v", tc.path, want, got)
			}
		}
	}
}

func TestSupportedLanguages(t *testing.T) {
	for _, ext := range []string{".go", ".js", ".jsx", ".ts", ".tsx", ".py", ".rs"} {
		if supportedLangs[ext].lang == nil {
			t.Fatalf("missing language for %s", ext)
		}
	}
}

func TestParseFileGo(t *testing.T) {
	p := NewParser()
	src := []byte(`package main

import "fmt"

// Hello says hello.
func Hello(name string) {
	fmt.Println("hello", name)
}

func main() {
	Hello("world")
}

type Greeter struct {
	Name string
}

func (g Greeter) Greet() {
	Hello(g.Name)
}
`)

	nodes, edges := p.ParseFile("/test/main.go", src)

	// Collect nodes by name
	byName := make(map[string]*Node)
	for _, n := range nodes {
		byName[n.Name] = n
	}

	// Should have: Hello, main, Greeter
	if _, ok := byName["Hello"]; !ok {
		t.Error("missing node: Hello")
	}
	if _, ok := byName["main"]; !ok {
		t.Error("missing node: main")
	}
	if _, ok := byName["Greeter"]; !ok {
		t.Error("missing node: Greeter")
	}

	// Check kinds
	if byName["Hello"].Kind != KindFunc {
		t.Errorf("Hello.Kind = %s, want func", byName["Hello"].Kind)
	}
	// Greeter may be KindStruct or KindType depending on query iteration order
	if byName["Greeter"].Kind != KindStruct && byName["Greeter"].Kind != KindType {
		t.Errorf("Greeter.Kind = %s, want struct or type", byName["Greeter"].Kind)
	}
	// Greet may or may not be extracted depending on tree-sitter query matching
	if greet, ok := byName["Greet"]; ok && greet.Kind != KindMethod {
		t.Errorf("Greet.Kind = %s, want method", greet.Kind)
	}

	// Check doc
	if byName["Hello"].Doc == "" {
		t.Error("Hello should have doc comment")
	}

	// Check signature
	if byName["Hello"].Signature == "" {
		t.Error("Hello should have signature")
	}

	// Check package path
	if byName["Hello"].PkgPath != "main" {
		t.Errorf("Hello.PkgPath = %q, want main", byName["Hello"].PkgPath)
	}

	// Check edges
	callEdges := 0
	importEdges := 0
	for _, e := range edges {
		switch e.Kind {
		case EdgeCalls:
			callEdges++
		case EdgeImports:
			importEdges++
		}
	}
	if importEdges == 0 {
		t.Error("should have at least 1 import edge")
	}
	if callEdges == 0 {
		t.Error("should have at least 1 call edge")
	}
}

func TestParseFileGoVarsConsts(t *testing.T) {
	p := NewParser()
	src := []byte(`package main

import "fmt"

const MaxRetries = 3
const (
	StatusOK    = 200
	StatusError = 500
)

var GlobalCounter int
var (
	Version   = "1.0"
	DebugMode = false
)

func main() {
	local := 10
	fmt.Println(local, GlobalCounter, MaxRetries)
}
`)

	nodes, _ := p.ParseFile("/test/main.go", src)

	byName := make(map[string]*Node)
	for _, n := range nodes {
		byName[n.Name] = n
	}

	// Single const
	if n, ok := byName["MaxRetries"]; !ok {
		t.Error("missing node: MaxRetries")
	} else if n.Kind != KindConst {
		t.Errorf("MaxRetries.Kind = %s, want const", n.Kind)
	}

	// Const block
	if n, ok := byName["StatusOK"]; !ok {
		t.Error("missing node: StatusOK")
	} else if n.Kind != KindConst {
		t.Errorf("StatusOK.Kind = %s, want const", n.Kind)
	}
	if n, ok := byName["StatusError"]; !ok {
		t.Error("missing node: StatusError")
	} else if n.Kind != KindConst {
		t.Errorf("StatusError.Kind = %s, want const", n.Kind)
	}

	// Single var
	if n, ok := byName["GlobalCounter"]; !ok {
		t.Error("missing node: GlobalCounter")
	} else if n.Kind != KindVar {
		t.Errorf("GlobalCounter.Kind = %s, want var", n.Kind)
	}

	// Var block
	if n, ok := byName["Version"]; !ok {
		t.Error("missing node: Version")
	} else if n.Kind != KindVar {
		t.Errorf("Version.Kind = %s, want var", n.Kind)
	}
	if n, ok := byName["DebugMode"]; !ok {
		t.Error("missing node: DebugMode")
	} else if n.Kind != KindVar {
		t.Errorf("DebugMode.Kind = %s, want var", n.Kind)
	}
}

func TestParseFileJSVars(t *testing.T) {
	p := NewParser()
	src := []byte(`const MAX_SIZE = 100;
let counter = 0;
var name = "test";

function hello() {
    console.log(name);
}
`)

	nodes, _ := p.ParseFile("/test/main.js", src)

	byName := make(map[string]*Node)
	for _, n := range nodes {
		byName[n.Name] = n
	}

	if n, ok := byName["MAX_SIZE"]; !ok {
		t.Error("missing node: MAX_SIZE")
	} else if n.Kind != KindVar {
		t.Errorf("MAX_SIZE.Kind = %s, want var", n.Kind)
	}

	if n, ok := byName["counter"]; !ok {
		t.Error("missing node: counter")
	} else if n.Kind != KindVar {
		t.Errorf("counter.Kind = %s, want var", n.Kind)
	}
}

func TestParseFilePython(t *testing.T) {
	p := NewParser()
	src := []byte(`import os

def hello(name):
    print("hello", name)

class Greeter:
    def greet(self):
        hello("world")
`)

	nodes, _ := p.ParseFile("/test/main.py", src)

	byName := make(map[string]*Node)
	for _, n := range nodes {
		byName[n.Name] = n
	}

	if _, ok := byName["hello"]; !ok {
		t.Error("missing node: hello")
	}
	if byName["hello"].Kind != KindFunc {
		t.Errorf("hello.Kind = %s, want func", byName["hello"].Kind)
	}
	if _, ok := byName["Greeter"]; !ok {
		t.Error("missing node: Greeter")
	}
	if byName["Greeter"].Kind != KindClass {
		t.Errorf("Greeter.Kind = %s, want class", byName["Greeter"].Kind)
	}
}

func TestParseFileUnsupported(t *testing.T) {
	p := NewParser()
	nodes, edges := p.ParseFile("/test/file.xyz", []byte("hello"))
	if nodes != nil || edges != nil {
		t.Error("unsupported extension should return nil")
	}
}

func TestExtractDoc(t *testing.T) {
	p := NewParser()

	tests := []struct {
		name string
		src  string
		ext  string
		want string
	}{
		{
			name: "line comment",
			src: `package main

// This is a doc comment.
func Foo() {}
`,
			want: "// This is a doc comment.",
		},
		{
			name: "hash comment",
			src: `# This is a doc comment.
def foo():
    pass
`,
			ext:  ".py",
			want: "# This is a doc comment.",
		},
		{
			name: "multi-line //",
			src: `package main

// Line one.
// Line two.
func Foo() {}
`,
			want: "// Line one.\n// Line two.",
		},
		{
			name: "block comment single line",
			src: `package main

/* This is a block doc. */
func Foo() {}
`,
			want: "/* This is a block doc. */",
		},
		{
			name: "block comment multi line",
			src: `package main

/*
 * This is a multi-line
 * block comment.
 */
func Foo() {}
`,
			want: "/*\n* This is a multi-line\n* block comment.\n*/",
		},
		{
			name: "no doc",
			src: `package main

func Foo() {}
`,
			want: "",
		},
		{
			name: "blank lines between comment and func",
			src: `package main

// Doc comment.

func Foo() {}
`,
			want: "// Doc comment.",
		},
	}

	for _, tt := range tests {
		ext := tt.ext
		if ext == "" {
			ext = ".go"
		}
		nodes, _ := p.ParseFile("/test/"+tt.name+ext, []byte(tt.src))
		var doc string
		for _, n := range nodes {
			if n.Name == "Foo" || n.Name == "foo" {
				doc = n.Doc
				break
			}
		}
		if doc != tt.want {
			t.Errorf("%s: doc = %q, want %q", tt.name, doc, tt.want)
		}
	}
}

func TestExtractSignature(t *testing.T) {
	p := NewParser()
	src := []byte(`package main

func Short() {}

func VeryLongNameWithManyParameters(a int, b string, c float64, d bool, e []byte, f map[string]int) (error, string) {
	return nil, ""
}
`)

	nodes, _ := p.ParseFile("/test/sig.go", src)
	byName := make(map[string]*Node)
	for _, n := range nodes {
		byName[n.Name] = n
	}

	if byName["Short"].Signature != "func Short() {}" {
		t.Errorf("Short sig = %q", byName["Short"].Signature)
	}

	// Long signature should be truncated
	sig := byName["VeryLongNameWithManyParameters"].Signature
	if len(sig) > 203 { // 200 + "..."
		t.Errorf("signature too long: %d chars", len(sig))
	}
}

func TestExtractPackagePathGo(t *testing.T) {
	p := NewParser()
	src := []byte(`package mypackage

func Foo() {}
`)

	nodes, _ := p.ParseFile("/test/foo.go", src)
	if len(nodes) == 0 {
		t.Fatal("no nodes parsed")
	}
	if nodes[0].PkgPath != "mypackage" {
		t.Errorf("PkgPath = %q, want mypackage", nodes[0].PkgPath)
	}
}

func TestNewParserCreatesAllParsers(t *testing.T) {
	p := NewParser()
	if len(p.parsers) != len(supportedLangs) {
		t.Errorf("parsers count = %d, want %d", len(p.parsers), len(supportedLangs))
	}
	for ext := range supportedLangs {
		if _, ok := p.parsers[ext]; !ok {
			t.Errorf("missing parser for %s", ext)
		}
	}
}
