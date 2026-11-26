package architecture_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// Parse source rather than invoking go list: this checks inactive platform
// files too, runs offline, and participates in the normal go test CI gate.
func TestDependencyBoundaries(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
	imports := make(map[string]map[string]bool)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if strings.HasPrefix(entry.Name(), ".") || entry.Name() == "node_modules" || entry.Name() == "vendor" || entry.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(root, filepath.Dir(path))
		if err != nil {
			return err
		}
		pkg := filepath.ToSlash(rel)
		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, spec := range parsed.Imports {
			dep, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			if !strings.HasPrefix(dep, "nekocode/") {
				continue
			}
			dep = strings.TrimPrefix(dep, "nekocode/")
			if imports[pkg] == nil {
				imports[pkg] = make(map[string]bool)
			}
			imports[pkg][dep] = true
			if forbiddenDependency(pkg, dep) {
				t.Errorf("%s: forbidden dependency %s -> %s", path, pkg, dep)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	checkModelClientDependencies(t, imports)
}

// Check the full local import graph: a helper that imports the factory also
// links its built-in clients into every harness that uses that helper.
func checkModelClientDependencies(t *testing.T, imports map[string]map[string]bool) {
	t.Helper()
	seen := make(map[string]bool)
	var visit func(string, []string)
	visit = func(pkg string, path []string) {
		if seen[pkg] {
			return
		}
		seen[pkg] = true
		path = append(path, pkg)
		if under(pkg, "bot/provider") && !under(pkg, "bot/provider/llm") && !under(pkg, "bot/provider/types") {
			t.Errorf("harness imports a model backend or factory: %s", strings.Join(path, " -> "))
			return
		}
		for dep := range imports[pkg] {
			visit(dep, path)
		}
	}
	for pkg := range imports {
		if under(pkg, "bot/agent") || under(pkg, "bot/contextmgr") || under(pkg, "bot/provider/llm") {
			visit(pkg, nil)
		}
	}
}

func forbiddenDependency(pkg, dep string) bool {
	switch {
	case pkg == "protocol", under(pkg, "bot/agent/internal/kernel"):
		return true // standalone packages have no project imports
	case under(pkg, "bot"):
		if under(dep, "runtime") || isAdapter(dep) || (!under(pkg, "bot/core") && under(dep, "bot/core")) {
			return true
		}
		switch {
		case under(pkg, "bot/agent"):
			return under(dep, "bot/session") || under(dep, "bot/extension/tool/builtin")
		case under(pkg, "bot/contextmgr"):
			return under(dep, "bot/session") || under(dep, "bot/policy") || under(dep, "bot/agent")
		case under(pkg, "bot/extension/tool/builtin/filesystem"):
			return under(dep, "bot/policy")
		}
	case pkg == "runtime", under(pkg, "runtime/internal"):
		return under(dep, "bot") || isAdapter(dep) || under(dep, "runtime/standard") || under(dep, "runtime/agentrunner")
	case isAdapter(pkg):
		return under(dep, "bot") || under(dep, "runtime/internal")
	}
	return false
}

func under(path, prefix string) bool { return path == prefix || strings.HasPrefix(path, prefix+"/") }

func isAdapter(pkg string) bool {
	return under(pkg, "acp") || under(pkg, "headless") || under(pkg, "interaction") || under(pkg, "runtime/httpapi") || under(pkg, "runtime/a2aapi")
}
