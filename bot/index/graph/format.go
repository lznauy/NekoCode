package graph

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// FormatSkeleton produces a compact project overview for Layer 0 injection.
func (g *Graph) FormatSkeleton(cwd string) string {
	if len(g.Nodes) == 0 {
		return ""
	}

	lang := detectLanguage(g)
	modPath := detectModule(g)

	var b strings.Builder
	b.WriteString("<project>\n")
	fmt.Fprintf(&b, "<language>%s</language>\n", lang)
	if modPath != "" {
		fmt.Fprintf(&b, "<module_path>%s</module_path>\n", modPath)
	}

	pkgs := make(map[string]bool)
	files := make(map[string]bool)
	for _, n := range g.Nodes {
		if n.PkgPath != "" {
			pkgs[n.PkgPath] = true
		}
		files[n.File] = true
	}
	fmt.Fprintf(&b, "<summary>%d packages, %d files, %d symbols</summary>\n",
		len(pkgs), len(files), len(g.Nodes))

	dirs := make(map[string]bool)
	for f := range files {
		rel := f
		if cwd != "" {
			if r, err := filepath.Rel(cwd, f); err == nil {
				rel = r
			}
		}
		dir := filepath.Dir(rel)
		parts := strings.Split(dir, string(filepath.Separator))
		var clean []string
		for _, p := range parts {
			if p != "" && p != "." && p != ".." {
				clean = append(clean, p)
			}
		}
		if len(clean) >= 2 {
			dirs[strings.Join(clean[:2], "/")] = true
		} else if len(clean) == 1 {
			dirs[clean[0]] = true
		}
	}
	sorted := make([]string, 0, len(dirs))
	for d := range dirs {
		sorted = append(sorted, d)
	}
	sort.Strings(sorted)
	b.WriteString("<top_dirs depth=\"2\">")
	for i, d := range sorted {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(d)
	}
	b.WriteString("</top_dirs>\n")

	if len(g.Edges) > 0 {
		depMap := make(map[string]map[string]bool)
		for _, n := range g.Nodes {
			if n.PkgPath == "" {
				continue
			}
			for _, e := range g.edgesByFrom[n.ID] {
				if e.Kind == EdgeImports {
					if to, ok := g.Nodes[e.ToID]; ok && to.PkgPath != "" && n.PkgPath != to.PkgPath {
						if depMap[n.PkgPath] == nil {
							depMap[n.PkgPath] = make(map[string]bool)
						}
						depMap[n.PkgPath][to.PkgPath] = true
					}
				}
			}
		}
		if len(depMap) > 0 {
			b.WriteString("<deps>\n")
			count := 0
			for pkg, deps := range depMap {
				if count >= 20 {
					break
				}
				var depList []string
				for d := range deps {
					depList = append(depList, d)
				}
				fmt.Fprintf(&b, "  %s → %s\n", pkg, strings.Join(depList, ", "))
				count++
			}
			b.WriteString("</deps>\n")
		}
	}

	b.WriteString("</project>\n")
	return b.String()
}
