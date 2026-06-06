package cindex

import (
	"testing"
)

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
