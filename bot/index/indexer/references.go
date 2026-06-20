package indexer

import "strings"

// ResolveReferences resolves cross-file call and import references.
// Exported so the Syncer can call it after incremental updates.
func (i *Indexer) ResolveReferences(g *Graph) {
	resolveReferences(g)
}

func resolveReferences(g *Graph) {
	nameIndex := make(map[string][]*Node)
	for _, n := range g.Nodes {
		nameIndex[n.Name] = append(nameIndex[n.Name], n)
	}

	for _, e := range g.Edges {
		oldToID := e.ToID

		switch e.Kind {
		case EdgeCalls:
			if e.CalleeName == "" || e.ToID != 0 {
				continue
			}
			candidates := nameIndex[e.CalleeName]
			if len(candidates) == 0 {
				continue
			}
			caller := g.Nodes[e.FromID]
			var best *Node
			for _, c := range candidates {
				if caller != nil && c.PkgPath == caller.PkgPath {
					best = c
					break
				}
			}
			if best == nil {
				best = candidates[0]
			}
			e.ToID = best.ID

		case EdgeImports:
			if e.ImportPath == "" || e.ToID != 0 {
				continue
			}
			for _, n := range g.Nodes {
				if n.PkgPath != "" && (e.ImportPath == n.PkgPath || strings.HasSuffix(e.ImportPath, "/"+n.PkgPath)) {
					e.ToID = n.ID
					break
				}
			}
		}

		if e.ToID != oldToID {
			g.ReindexEdgeTo(e, oldToID)
		}
	}
}
