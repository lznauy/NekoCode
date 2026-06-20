package graph

import "strings"

// FileCount returns the number of indexed file metadata entries.
func (g *Graph) FileCount() int {
	return len(g.files)
}

// EdgeFromCount returns the number of indexed outgoing edges for a node.
func (g *Graph) EdgeFromCount(nodeID int64) int {
	return len(g.edgesByFrom[nodeID])
}

// EdgeToCount returns the number of indexed incoming edges for a node.
func (g *Graph) EdgeToCount(nodeID int64) int {
	return len(g.edgesByTo[nodeID])
}

// HasFileInfo reports whether indexed file metadata exists for path.
func (g *Graph) HasFileInfo(path string) bool {
	_, ok := g.files[path]
	return ok
}

// FindNodesByName finds nodes matching a name.
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

// QuerySymbol finds symbols matching a name.
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

// QueryDeps returns dependencies for a package.
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

// QueryFile finds files matching a path fragment.
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
