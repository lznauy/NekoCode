package cindex

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// NodeKind represents the type of a code symbol.
type NodeKind string

const (
	KindFunc      NodeKind = "func"
	KindMethod    NodeKind = "method"
	KindType      NodeKind = "type"
	KindStruct    NodeKind = "struct"
	KindInterface NodeKind = "interface"
	KindClass     NodeKind = "class"
	KindVar       NodeKind = "var"
	KindConst     NodeKind = "const"
	KindFile      NodeKind = "file"
)

// EdgeKind represents the type of a relationship between nodes.
type EdgeKind string

const (
	EdgeCalls    EdgeKind = "calls"
	EdgeContains EdgeKind = "contains"
	EdgeImports  EdgeKind = "imports"
)

// Node represents a code symbol (function, class, variable, etc.).
type Node struct {
	ID         int64    `json:"id"`
	Name       string   `json:"name"`
	Kind       NodeKind `json:"kind"`
	File       string   `json:"file"`
	Line       int      `json:"line"`
	EndLine    int      `json:"end_line"`
	PkgPath    string   `json:"pkg_path"`
	Signature  string   `json:"signature,omitempty"`
	Doc        string   `json:"doc,omitempty"`
	Visibility string   `json:"visibility"` // public, private, protected
}

// Edge represents a relationship between two nodes.
type Edge struct {
	ID         int64    `json:"id"`
	FromID     int64    `json:"from_id"`
	ToID       int64    `json:"to_id"`
	Kind       EdgeKind `json:"kind"`
	File       string   `json:"file,omitempty"`
	Line       int      `json:"line,omitempty"`
	CalleeName string   `json:"callee_name,omitempty"` // unresolved callee name for call edges
	ImportPath string   `json:"import_path,omitempty"` // unresolved import path for import edges
}

// FileInfo tracks indexed files and their content hashes.
type FileInfo struct {
	Path        string `json:"path"`
	ContentHash string `json:"content_hash"`
	Language    string `json:"language"`
}

// Graph is the in-memory code knowledge graph.
type Graph struct {
	Nodes map[int64]*Node
	Edges map[int64]*Edge

	// Indexes for fast lookup
	nodesByName map[string][]*Node          // name → nodes
	nodesByFile map[string][]*Node          // file → nodes
	edgesByFrom map[int64][]*Edge           // fromID → edges
	edgesByTo   map[int64][]*Edge           // toID → edges
	files       map[string]*FileInfo        // path → file info

	nextNodeID int64
	nextEdgeID int64
}

// NewGraph creates an empty graph.
func NewGraph() *Graph {
	return &Graph{
		Nodes:       make(map[int64]*Node),
		Edges:       make(map[int64]*Edge),
		nodesByName: make(map[string][]*Node),
		nodesByFile: make(map[string][]*Node),
		edgesByFrom: make(map[int64][]*Edge),
		edgesByTo:   make(map[int64][]*Edge),
		files:       make(map[string]*FileInfo),
	}
}

// AddNode adds a node to the graph and returns its ID.
func (g *Graph) AddNode(n *Node) int64 {
	if n.ID == 0 {
		g.nextNodeID++
		n.ID = g.nextNodeID
	}
	g.Nodes[n.ID] = n
	g.nodesByName[n.Name] = append(g.nodesByName[n.Name], n)
	g.nodesByFile[n.File] = append(g.nodesByFile[n.File], n)
	if n.ID >= g.nextNodeID {
		g.nextNodeID = n.ID
	}
	return n.ID
}

// AddEdge adds an edge to the graph and returns its ID.
func (g *Graph) AddEdge(e *Edge) int64 {
	if e.ID == 0 {
		g.nextEdgeID++
		e.ID = g.nextEdgeID
	}
	g.Edges[e.ID] = e
	g.edgesByFrom[e.FromID] = append(g.edgesByFrom[e.FromID], e)
	g.edgesByTo[e.ToID] = append(g.edgesByTo[e.ToID], e)
	if e.ID >= g.nextEdgeID {
		g.nextEdgeID = e.ID
	}
	return e.ID
}

// RemoveNode removes a node and all its edges.
func (g *Graph) RemoveNode(id int64) {
	n, ok := g.Nodes[id]
	if !ok {
		return
	}

	// Remove from indexes
	nodes := g.nodesByName[n.Name]
	for i, nd := range nodes {
		if nd.ID == id {
			g.nodesByName[n.Name] = append(nodes[:i], nodes[i+1:]...)
			break
		}
	}
	nodes = g.nodesByFile[n.File]
	for i, nd := range nodes {
		if nd.ID == id {
			g.nodesByFile[n.File] = append(nodes[:i], nodes[i+1:]...)
			break
		}
	}

	// Remove edges
	for _, e := range g.edgesByFrom[id] {
		delete(g.Edges, e.ID)
		// Remove from edgesByTo
		toEdges := g.edgesByTo[e.ToID]
		for i, edge := range toEdges {
			if edge.ID == e.ID {
				g.edgesByTo[e.ToID] = append(toEdges[:i], toEdges[i+1:]...)
				break
			}
		}
	}
	for _, e := range g.edgesByTo[id] {
		delete(g.Edges, e.ID)
		fromEdges := g.edgesByFrom[e.FromID]
		for i, edge := range fromEdges {
			if edge.ID == e.ID {
				g.edgesByFrom[e.FromID] = append(fromEdges[:i], fromEdges[i+1:]...)
				break
			}
		}
	}
	delete(g.edgesByFrom, id)
	delete(g.edgesByTo, id)
	delete(g.Nodes, id)
}

// RemoveFileNodes removes all nodes and edges from a specific file.
func (g *Graph) RemoveFileNodes(file string) {
	// Copy the slice before iterating — RemoveNode mutates g.nodesByFile[file] in place,
	// which would corrupt the slice we're ranging over.
	nodes := append([]*Node(nil), g.nodesByFile[file]...)
	for _, n := range nodes {
		g.RemoveNode(n.ID)
	}
	delete(g.nodesByFile, file)
	delete(g.files, file)
}

// FindNodesByName finds nodes matching a name (partial match, case-insensitive).
func (g *Graph) FindNodesByName(name string) []*Node {
	var result []*Node
	lower := strings.ToLower(name)
	seen := make(map[int64]bool)
	for _, nodes := range g.nodesByName {
		for _, n := range nodes {
			if !seen[n.ID] && strings.Contains(strings.ToLower(n.Name), lower) {
				seen[n.ID] = true
				result = append(result, n)
			}
		}
	}
	return result
}

// FindNodesByFile finds nodes in files matching a path fragment.
func (g *Graph) FindNodesByFile(name string) []*Node {
	var result []*Node
	for file, nodes := range g.nodesByFile {
		if strings.Contains(file, name) {
			result = append(result, nodes...)
		}
	}
	if len(result) > 15 {
		result = result[:15]
	}
	return result
}

// nodesFromEdges returns nodes reachable from edges in edgesByTo[id] filtered by kind.
func (g *Graph) nodesFromEdges(id int64, kind EdgeKind) []*Node {
	var result []*Node
	for _, e := range g.edgesByTo[id] {
		if e.Kind == kind {
			if n, ok := g.Nodes[e.FromID]; ok {
				result = append(result, n)
			}
		}
	}
	return result
}

// nodesToEdges returns nodes reachable from edges in edgesByFrom[id] filtered by kind.
func (g *Graph) nodesToEdges(id int64, kind EdgeKind) []*Node {
	var result []*Node
	for _, e := range g.edgesByFrom[id] {
		if e.Kind == kind {
			if n, ok := g.Nodes[e.ToID]; ok {
				result = append(result, n)
			}
		}
	}
	return result
}

// firstFromEdge returns the first node in edgesByTo[id] matching kind, or nil.
func (g *Graph) firstFromEdge(id int64, kind EdgeKind) *Node {
	for _, e := range g.edgesByTo[id] {
		if e.Kind == kind {
			if n, ok := g.Nodes[e.FromID]; ok {
				return n
			}
		}
	}
	return nil
}

// GetCallers returns all nodes that call the given node.
func (g *Graph) GetCallers(nodeID int64) []*Node { return g.nodesFromEdges(nodeID, EdgeCalls) }

// GetCallees returns all nodes called by the given node.
func (g *Graph) GetCallees(nodeID int64) []*Node { return g.nodesToEdges(nodeID, EdgeCalls) }

// GetChildren returns all nodes contained by the given node.
func (g *Graph) GetChildren(nodeID int64) []*Node { return g.nodesToEdges(nodeID, EdgeContains) }

// GetParent returns the node that contains the given node.
func (g *Graph) GetParent(nodeID int64) *Node { return g.firstFromEdge(nodeID, EdgeContains) }

// FormatSkeleton produces a compact project overview for Layer 0 injection.
// This is compatible with the old projctx.ProjectIndex.FormatSkeleton().
// cwd is used to compute relative paths for the directory tree; pass "" to keep absolute paths.
func (g *Graph) FormatSkeleton(cwd string) string {
	if len(g.Nodes) == 0 {
		return ""
	}

	// Detect language and module from nodes
	lang := detectLanguage(g)
	modPath := detectModule(g)

	var b strings.Builder
	b.WriteString("<project>\n")
	fmt.Fprintf(&b, "<language>%s</language>\n", lang)
	if modPath != "" {
		fmt.Fprintf(&b, "<module_path>%s</module_path>\n", modPath)
	}

	// Collect packages and files in one pass
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

	// Directory tree — first two levels (relative to cwd)
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
		// Filter out ".." and "." components
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

	// Dep graph (from imports edges, using edgesByFrom index)
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

// QuerySymbol finds symbols matching a name (for project_info tool compatibility).
func (g *Graph) QuerySymbol(name string) []SymbolInfo {
	nodes := g.FindNodesByName(name)
	result := make([]SymbolInfo, 0, len(nodes))
	for _, n := range nodes {
		result = append(result, SymbolInfo{
			Name:    n.Name,
			Kind:    string(n.Kind),
			File:    n.File,
			Line:    n.Line,
			PkgPath: n.PkgPath,
		})
	}
	return result
}

// QueryDeps returns dependencies for a package (for project_info tool compatibility).
func (g *Graph) QueryDeps(pkgPath string) []string {
	deps := make(map[string]bool)
	for _, n := range g.Nodes {
		if n.PkgPath != pkgPath {
			continue
		}
		for _, e := range g.edgesByFrom[n.ID] {
			if e.Kind == EdgeImports {
				if to, ok := g.Nodes[e.ToID]; ok && to.PkgPath != pkgPath {
					deps[to.PkgPath] = true
				}
			}
		}
	}
	if len(deps) == 0 {
		return nil
	}
	result := make([]string, 0, len(deps))
	for d := range deps {
		result = append(result, d)
	}
	return result
}

// QueryFile finds files matching a path fragment (case-insensitive).
func (g *Graph) QueryFile(name string) []FileInfo {
	var result []FileInfo
	seen := make(map[string]bool)
	lower := strings.ToLower(name)
	for _, n := range g.Nodes {
		if !seen[n.File] && strings.Contains(strings.ToLower(n.File), lower) {
			seen[n.File] = true
			result = append(result, FileInfo{Path: n.File})
		}
	}
	return result
}

// SymbolInfo is a compatibility type matching projctx.SymbolInfo.
type SymbolInfo struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	File    string `json:"file"`
	Line    int    `json:"line"`
	PkgPath string `json:"pkg_path"`
}

func detectLanguage(g *Graph) string {
	langs := make(map[string]int)
	for _, n := range g.Nodes {
		ext := filepath.Ext(n.File)
		if lang := detectLanguageForFile(ext); lang != "unknown" {
			langs[lang]++
		}
	}
	maxCount := 0
	lang := "unknown"
	for l, c := range langs {
		if c > maxCount {
			maxCount = c
			lang = l
		}
	}
	return lang
}

func detectModule(g *Graph) string {
	// For Go, try to find module path from package paths
	for _, n := range g.Nodes {
		if n.PkgPath != "" && strings.Contains(n.PkgPath, ".") {
			// Looks like a Go module path
			parts := strings.Split(n.PkgPath, "/")
			if len(parts) >= 3 {
				return strings.Join(parts[:3], "/")
			}
			return n.PkgPath
		}
	}
	return ""
}
