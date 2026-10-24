package indexer

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"

	graphpkg "nekocode/bot/extension/tool/builtin/index/core/internal/graph"
)

// FileChange is the final state of one file in a watcher batch.
type FileChange struct {
	Path    string
	Content []byte
	Deleted bool
}

// DeleteFile removes a file from both persistent index storage and the graph.
func (i *Indexer) DeleteFile(g *graphpkg.Graph, path string) error {
	return i.ApplyChanges(g, "", []FileChange{{Path: path, Deleted: true}})
}

// UpsertFile updates one file and resolves references before returning.
func (i *Indexer) UpsertFile(g *graphpkg.Graph, cwd, path string, content []byte) error {
	return i.ApplyChanges(g, cwd, []FileChange{{Path: path, Content: content}})
}

// ApplyChanges commits files independently and resolves references once for the
// batch. Earlier successful updates remain usable if a later file fails.
// Callers must hold the graph's write lock throughout this operation.
func (i *Indexer) ApplyChanges(g *graphpkg.Graph, cwd string, changes []FileChange) (err error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.applyChangesLocked(g, cwd, changes)
}

func (i *Indexer) applyChangesLocked(g *graphpkg.Graph, cwd string, changes []FileChange) (err error) {
	changed := false
	defer func() {
		if changed && g != nil {
			resolveReferences(g)
		}
	}()
	for _, change := range changes {
		if change.Deleted {
			if err = i.db.DeleteFile(change.Path); err != nil {
				return err
			}
			if g != nil {
				g.DetachFileReferences(change.Path)
				g.RemoveFileNodes(change.Path)
			}
			changed = true
			continue
		}
		hash := fmt.Sprintf("%x", sha256.Sum256(change.Content))
		if i.db.GetFileHash(change.Path) == hash {
			continue
		}
		if g == nil {
			// Preserve metadata-only updates while removing stale symbols atomically.
			if err = i.db.ReplaceFile(change.Path, hash, graphpkg.LanguageForFile(filepath.Ext(change.Path)), nil, nil); err != nil {
				return err
			}
			continue
		}
		nodes, edges := i.parser.ParseFile(change.Path, change.Content)
		nodes, edges, lang := prepareFile(g, change.Path, cwd, nodes, edges)
		if err = i.db.ReplaceFile(change.Path, hash, lang, nodes, edges); err != nil {
			return fmt.Errorf("index %s: %w", change.Path, err)
		}
		publishFile(g, change.Path, hash, lang, nodes, edges)
		changed = true
	}
	return nil
}

// prepareFile assigns graph IDs to parsed symbols without changing the graph.
func prepareFile(g *graphpkg.Graph, path, cwd string, nodes []*graphpkg.Node, edges []*graphpkg.Edge) ([]*graphpkg.Node, []*graphpkg.Edge, string) {
	ext := filepath.Ext(path)
	lang := graphpkg.LanguageForFile(ext)
	pkgPath, _ := filepath.Rel(cwd, filepath.Dir(path))
	if pkgPath == "." {
		pkgPath = ""
	}
	if ext == ".go" && pkgPath == "" && len(nodes) > 0 {
		pkgPath = nodes[0].PkgPath
	}
	nextNode, nextEdge := g.NextIDs()
	fileNode := &graphpkg.Node{ID: nextNode, Name: filepath.Base(path), Kind: graphpkg.KindFile, File: path, PkgPath: pkgPath}
	allNodes := make([]*graphpkg.Node, 0, len(nodes)+1)
	allNodes = append(allNodes, fileNode)
	parserIDs := make(map[int64]int64, len(nodes))
	for idx, n := range nodes {
		nextNode++
		n.ID = nextNode
		n.PkgPath = pkgPath
		parserIDs[-int64(idx+1)] = n.ID
		allNodes = append(allNodes, n)
	}
	validEdges := make([]*graphpkg.Edge, 0, len(edges))
	for _, e := range edges {
		if e.FromID < 0 {
			id, ok := parserIDs[e.FromID]
			if !ok {
				continue
			}
			e.FromID = id
		}
		if e.FromID == 0 {
			if e.Kind != graphpkg.EdgeImports {
				continue
			}
			e.FromID = fileNode.ID
		}
		e.ID = nextEdge
		nextEdge++
		validEdges = append(validEdges, e)
	}
	return allNodes, validEdges, lang
}

func publishFile(g *graphpkg.Graph, path, hash, lang string, nodes []*graphpkg.Node, edges []*graphpkg.Edge) {
	g.DetachFileReferences(path)
	g.RemoveFileNodes(path)
	for _, n := range nodes {
		g.AddNode(n)
	}
	for _, e := range edges {
		g.AddEdge(e)
	}
	g.SetFileInfo(path, hash, lang)
}
