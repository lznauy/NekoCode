package indexer

import (
	"strings"

	graphpkg "nekocode/bot/extension/tool/builtin/index/core/internal/graph"
)

// ResolveReferences resolves cross-file call and import references.
// Exported so the Syncer can call it after incremental updates.
func (i *Indexer) ResolveReferences(g *graphpkg.Graph) {
	resolveReferences(g)
}

func resolveReferences(g *graphpkg.Graph) {
	nameIndex := make(map[string][]*graphpkg.Node)
	packageIndex := make(map[string]*graphpkg.Node)
	for _, n := range g.Nodes {
		nameIndex[n.Name] = append(nameIndex[n.Name], n)
		if n.PkgPath != "" {
			if old := packageIndex[n.PkgPath]; old == nil || n.ID < old.ID {
				packageIndex[n.PkgPath] = n
			}
		}
	}

	for _, e := range g.Edges {
		oldToID := e.ToID

		switch e.Kind {
		case graphpkg.EdgeCalls:
			if e.CalleeName == "" || e.ToID != 0 {
				continue
			}
			candidates := nameIndex[e.CalleeName]
			if len(candidates) == 0 {
				continue
			}
			caller := g.Nodes[e.FromID]
			var best *graphpkg.Node
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

		case graphpkg.EdgeImports:
			if e.ImportPath == "" || e.ToID != 0 {
				continue
			}
			// Prefer an exact package path, then the longest slash-delimited
			// suffix. External packages cost path lookups rather than a graph scan.
			for suffix := e.ImportPath; suffix != ""; {
				if n := packageIndex[suffix]; n != nil {
					e.ToID = n.ID
					break
				}
				_, rest, ok := strings.Cut(suffix, "/")
				if !ok {
					break
				}
				suffix = rest
			}
		}

		if e.ToID != oldToID {
			g.ReindexEdgeTo(e, oldToID)
		}
	}
}
