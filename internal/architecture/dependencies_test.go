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
			forbidden := pkg == "protocol" ||
				(under(pkg, "bot") && (under(dep, "runtime") || under(dep, "interaction") || dep == "acp" || dep == "headless")) ||
				(under(pkg, "bot") && !under(pkg, "bot/core") && under(dep, "bot/core")) ||
				((pkg == "runtime" || under(pkg, "runtime/internal")) && (under(dep, "bot") || under(dep, "interaction") || dep == "acp" || dep == "headless")) ||
				(isAdapter(pkg) && (under(dep, "bot") || under(dep, "runtime/internal")))
			if forbidden {
				t.Errorf("%s: forbidden dependency %s -> %s", path, pkg, dep)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func under(path, prefix string) bool { return path == prefix || strings.HasPrefix(path, prefix+"/") }

func isAdapter(pkg string) bool {
	return under(pkg, "acp") || under(pkg, "headless") || under(pkg, "interaction") || under(pkg, "runtime/httpapi")
}
