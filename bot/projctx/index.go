package projctx

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"golang.org/x/sync/singleflight"
)

// ProjectIndex holds pre-computed structural knowledge about a codebase.
// Injected into Layer 0 so the agent knows the project shape without exploring.
type ProjectIndex struct {
	Language  string              `json:"language"`
	Module    string              `json:"module"`
	Packages  []PkgInfo           `json:"packages"`
	Files     []FileInfo          `json:"files"`
	Symbols   []SymbolInfo        `json:"symbols"`
	Deps      map[string][]string `json:"deps"` // pkg path → deps (internal only)
}

// PkgInfo describes a single Go package.
type PkgInfo struct {
	Name  string   `json:"name"`
	Path  string   `json:"path"`
	Dir   string   `json:"dir"`
	Files []string `json:"files"`
}

// FileInfo describes a single source file.
type FileInfo struct {
	Path    string `json:"path"`
	Package string `json:"package"`
	Lines   int    `json:"lines"`
}

// SymbolInfo describes an exported symbol.
type SymbolInfo struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"` // "func", "type", "struct", "interface"
	File    string `json:"file"`
	Line    int    `json:"line"`
	PkgPath string `json:"pkg_path"`
}

// ---------------------------------------------------------------------------
// construction
// ---------------------------------------------------------------------------

var (
	buildGroup singleflight.Group

	// globalIgnoreDirs matches directories that must never be scanned.
	globalIgnoreDirs = []string{
		"node_modules", "vendor", "target", ".git", "dist", "build",
		"__pycache__", ".cache", ".next", ".turbo", "coverage",
		"testdata", ".nekocode",
	}

	// goGeneratedPatterns matches generated Go files that must be excluded.
	goGeneratedPatterns = regexp.MustCompile(`\.(pb|mock|gen)\.go$`)

	// exportedSymbolsRE extracts exported Go symbols by line prefix.
	funcRE = regexp.MustCompile(`^func ([A-Z]\w*)`)
	typeRE = regexp.MustCompile(`^type ([A-Z]\w*)\b`)
)

// IndexProject builds a ProjectIndex for the Go project at cwd.
// Results are cached to ~/.nekocode/index/{key}.json with a git-aware key.
func IndexProject(cwd string) (*ProjectIndex, error) {
	// Resolve to absolute for stable caching.
	cwd, err := filepath.Abs(cwd)
	if err != nil {
		return nil, err
	}

	modDir, modPath, err := findGoModule(cwd)
	if err != nil {
		return nil, err
	}

	cacheKey, err := computeCacheKey(modDir)
	if err != nil {
		return nil, err
	}

	cacheDir, err := indexCacheDir()
	if err != nil {
		return nil, err
	}
	cacheFile := filepath.Join(cacheDir, cacheKey+".json")

	// Single-flight: if another goroutine is already building this index, wait for it.
	v, err, _ := buildGroup.Do(cacheFile, func() (interface{}, error) {
		// Try cache first.
		if idx := loadCachedIndex(cacheFile); idx != nil {
			return idx, nil
		}
		// Build from scratch.
		idx, err := buildIndex(cwd, modDir, modPath)
		if err != nil {
			return nil, err
		}
		saveCachedIndex(cacheFile, idx)
		return idx, nil
	})
	if err != nil {
		return nil, err
	}
	return v.(*ProjectIndex), nil
}

// ---------------------------------------------------------------------------
// cache
// ---------------------------------------------------------------------------

func indexCacheDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".nekocode", "index")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

func computeCacheKey(modDir string) (string, error) {
	h := sha256.New()

	// go.mod content.
	modData, err := os.ReadFile(filepath.Join(modDir, "go.mod"))
	if err != nil {
		return "", err
	}
	h.Write(modData)

	// go.sum content (may not exist).
	sumData, _ := os.ReadFile(filepath.Join(modDir, "go.sum"))
	h.Write(sumData)

	// Git HEAD commit ID.
	gitDir := filepath.Join(modDir, ".git")
	if head, err := os.ReadFile(filepath.Join(gitDir, "HEAD")); err == nil {
		h.Write(head)
		// If HEAD is a ref, try to read the ref file.
		ref := strings.TrimPrefix(strings.TrimSpace(string(head)), "ref: ")
		if ref != "" && ref != string(head) {
			refData, _ := os.ReadFile(filepath.Join(gitDir, ref))
			h.Write(refData)
		}
	}

	// Git status porcelain for uncommitted changes.
	cmd := exec.Command("git", "-C", modDir, "status", "--porcelain")
	if out, err := cmd.Output(); err == nil {
		h.Write(out)
	}

	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

func loadCachedIndex(path string) *ProjectIndex {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var idx ProjectIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil
	}
	// Cache entries older than 1 hour are considered stale.
	info, err := os.Stat(path)
	if err != nil {
		return nil
	}
	if time.Since(info.ModTime()) > time.Hour {
		return nil
	}
	return &idx
}

func saveCachedIndex(path string, idx *ProjectIndex) {
	data, err := json.Marshal(idx)
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o644)
}

// ---------------------------------------------------------------------------
// build
// ---------------------------------------------------------------------------

func buildIndex(cwd, modDir, modPath string) (*ProjectIndex, error) {
	idx := &ProjectIndex{
		Language: "go",
		Module:   modPath,
		Deps:     make(map[string][]string),
	}

	// 1. go list for package structure + dependencies.
	pkgs, pkgImports, internalPkgs, err := listPackages(modDir, modPath)
	if err != nil {
		// If go list fails, fall back to a lightweight file-tree scan.
		return buildLightweightIndex(cwd, modPath)
	}
	idx.Packages = pkgs

	// 2. Walk source files, scan symbols.
	fileSet := make(map[string]bool)
	for _, p := range pkgs {
		for _, f := range p.Files {
			abs := filepath.Join(p.Dir, f)
			if fileSet[abs] {
				continue
			}
			fileSet[abs] = true

			info, err := os.Stat(abs)
			if err != nil {
				continue
			}
			lines := estimateLines(info.Size())
			idx.Files = append(idx.Files, FileInfo{
				Path:    abs,
				Package: p.Path,
				Lines:   lines,
			})

			// Scan symbols only for non-generated .go files.
			if strings.HasSuffix(f, ".go") && !goGeneratedPatterns.MatchString(f) {
				scanSymbols(abs, p.Path, idx)
			}
		}
	}

	// 3. Build dep graph — internal only, external first-level.
	for _, p := range pkgs {
		imports := pkgImports[p.Path]
		var deps []string
		for _, imp := range imports {
			if internalPkgs[imp] {
				deps = append(deps, imp)
			}
		}
		for _, imp := range imports {
			if !internalPkgs[imp] && !isStdlib(imp) {
				deps = append(deps, imp)
			}
		}
		sort.Strings(deps)
		idx.Deps[p.Path] = deps
	}

	return idx, nil
}

func listPackages(modDir, modPath string) ([]PkgInfo, map[string][]string, map[string]bool, error) {
	cmd := exec.Command("go", "list", "-json", "-e", "./...")
	cmd.Dir = modDir
	out, err := cmd.Output()
	if err != nil {
		return nil, nil, nil, err
	}

	decoder := json.NewDecoder(strings.NewReader(string(out)))
	var result []PkgInfo
	pkgImports := make(map[string][]string)
	internalPkgs := make(map[string]bool)

	for decoder.More() {
		var raw struct {
			Name       string
			ImportPath string
			Dir        string
			GoFiles    []string
			Imports    []string
		}
		if err := decoder.Decode(&raw); err != nil {
			continue
		}
		if raw.Name == "" {
			continue
		}

		// Only include packages under the module prefix.
		if !strings.HasPrefix(raw.ImportPath, modPath) {
			continue
		}

		internalPkgs[raw.ImportPath] = true
		pkgImports[raw.ImportPath] = raw.Imports
		result = append(result, PkgInfo{
			Name:  raw.Name,
			Path:  raw.ImportPath,
			Dir:   raw.Dir,
			Files: raw.GoFiles,
		})
	}

	return result, pkgImports, internalPkgs, nil
}

func scanSymbols(filePath, pkgPath string, idx *ProjectIndex) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return
	}
	lines := strings.Split(string(data), "\n")
	for lineNo, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "//") {
			continue
		}
		if m := funcRE.FindStringSubmatch(line); m != nil {
			idx.Symbols = append(idx.Symbols, SymbolInfo{
				Name: m[1], Kind: "func", File: filePath, Line: lineNo + 1, PkgPath: pkgPath,
			})
			continue
		}
		if m := typeRE.FindStringSubmatch(line); m != nil {
			kind := "type"
			if strings.Contains(line, "struct") {
				kind = "struct"
			} else if strings.Contains(line, "interface") {
				kind = "interface"
			}
			idx.Symbols = append(idx.Symbols, SymbolInfo{
				Name: m[1], Kind: kind, File: filePath, Line: lineNo + 1, PkgPath: pkgPath,
			})
		}
	}
}

func buildLightweightIndex(cwd, modPath string) (*ProjectIndex, error) {
	idx := &ProjectIndex{
		Language: "go",
		Module:   modPath,
		Deps:     make(map[string][]string),
	}
	// Walk the tree manually, respecting ignores.
	_ = filepath.WalkDir(cwd, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			for _, ignore := range globalIgnoreDirs {
				if name == ignore {
					return filepath.SkipDir
				}
			}
			if strings.HasPrefix(name, ".") && name != "." {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") || goGeneratedPatterns.MatchString(name) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		idx.Files = append(idx.Files, FileInfo{
			Path:  path,
			Lines: estimateLines(info.Size()),
		})
		scanSymbols(path, "", idx)
		return nil
	})
	return idx, nil
}

func findGoModule(cwd string) (modDir, modPath string, err error) {
	dir := cwd
	for {
		modFile := filepath.Join(dir, "go.mod")
		if data, e := os.ReadFile(modFile); e == nil {
			// Extract module path from go.mod.
			for _, line := range strings.Split(string(data), "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "module ") {
					return dir, strings.TrimSpace(strings.TrimPrefix(line, "module ")), nil
				}
			}
			return dir, "", fmt.Errorf("go.mod found but no module directive")
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", "", fmt.Errorf("no go.mod found in any ancestor of %s", cwd)
		}
		dir = parent
	}
}

func estimateLines(size int64) int {
	// Rough: average Go source line is ~40 bytes.
	n := int(size / 40)
	if n < 1 {
		n = 1
	}
	return n
}

func isStdlib(path string) bool {
	// Go standard library packages have no dot in the first path element.
	first := path
	if i := strings.IndexByte(path, '/'); i >= 0 {
		first = path[:i]
	}
	return !strings.Contains(first, ".")
}

// ---------------------------------------------------------------------------
// query helpers (for project_info tool)
// ---------------------------------------------------------------------------

// QuerySymbol finds symbols matching a prefix or exact name.
func (idx *ProjectIndex) QuerySymbol(name string) []SymbolInfo {
	var result []SymbolInfo
	exact := false
	for _, s := range idx.Symbols {
		if s.Name == name {
			exact = true
			result = append(result, s)
		}
	}
	if !exact {
		lower := strings.ToLower(name)
		for _, s := range idx.Symbols {
			if strings.Contains(strings.ToLower(s.Name), lower) {
				result = append(result, s)
			}
		}
	}
	if len(result) > 20 {
		result = result[:20]
	}
	return result
}

// QueryDeps returns the dependencies for a given package path.
func (idx *ProjectIndex) QueryDeps(pkgPath string) []string {
	return idx.Deps[pkgPath]
}

// QueryFile finds files matching a basename or path fragment.
func (idx *ProjectIndex) QueryFile(name string) []FileInfo {
	var result []FileInfo
	for _, f := range idx.Files {
		if strings.Contains(f.Path, name) {
			result = append(result, f)
		}
	}
	if len(result) > 15 {
		result = result[:15]
	}
	return result
}

// ---------------------------------------------------------------------------
// formatting for Layer 0 injection
// ---------------------------------------------------------------------------

// FormatSkeleton produces the compact Layer 0 text block (~500–1000 tokens).
func (idx *ProjectIndex) FormatSkeleton() string {
	var b strings.Builder

	b.WriteString("<project>\n")
	fmt.Fprintf(&b, "<lang>%s</lang>\n", idx.Language)
	fmt.Fprintf(&b, "<module>%s</module>\n", idx.Module)
	fmt.Fprintf(&b, "<stats>%d packages, %d files, %d symbols</stats>\n",
		len(idx.Packages), len(idx.Files), len(idx.Symbols))

	// Directory tree — first two levels only.
	dirs := make(map[string]bool)
	for _, f := range idx.Files {
		rel := strings.TrimPrefix(f.Path, idx.prefix())
		dir := filepath.Dir(rel)
		parts := strings.Split(dir, string(filepath.Separator))
		if len(parts) >= 2 {
			dirs[strings.Join(parts[:2], "/")] = true
		} else if len(parts) == 1 && parts[0] != "." {
			dirs[parts[0]] = true
		}
	}
	sorted := make([]string, 0, len(dirs))
	for d := range dirs {
		sorted = append(sorted, d)
	}
	sort.Strings(sorted)
	b.WriteString("<top>")
	for i, d := range sorted {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(d)
	}
	b.WriteString("</top>\n")

	// Internal dep graph — minimal.
	if len(idx.Deps) > 0 {
		b.WriteString("<deps>\n")
		count := 0
		for pkg, deps := range idx.Deps {
			if count >= 20 {
				break
			}
			short := strings.TrimPrefix(pkg, idx.Module+"/")
			var internal []string
			for _, d := range deps {
				if strings.HasPrefix(d, idx.Module) {
					internal = append(internal, strings.TrimPrefix(d, idx.Module+"/"))
				}
			}
			if len(internal) > 0 {
				fmt.Fprintf(&b, "  %s → %s\n", short, strings.Join(internal, ", "))
				count++
			}
		}
		b.WriteString("</deps>\n")
	}

	b.WriteString("</project>\n")
	return b.String()
}

func (idx *ProjectIndex) prefix() string {
	// Find common directory prefix from all files.
	if len(idx.Files) == 0 {
		return ""
	}
	prefix := filepath.Dir(idx.Files[0].Path)
	for _, f := range idx.Files[1:] {
		d := filepath.Dir(f.Path)
		for !strings.HasPrefix(d, prefix) {
			prefix = filepath.Dir(prefix)
			if prefix == "/" || prefix == "." {
				return ""
			}
		}
	}
	return prefix
}
