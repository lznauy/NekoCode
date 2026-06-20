package indexer

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"
)

// DeleteFile removes a file from both persistent index storage and the graph.
func (i *Indexer) DeleteFile(g *Graph, path string) error {
	if err := i.db.DeleteFile(path); err != nil {
		return err
	}
	if g != nil {
		g.RemoveFileNodes(path)
	}
	return nil
}

// UpsertFile parses one file, replaces existing index entries, and resolves references.
func (i *Indexer) UpsertFile(g *Graph, cwd, path string, content []byte) error {
	hash := fmt.Sprintf("%x", sha256.Sum256(content))
	if i.db.GetFileHash(path) == hash {
		return nil
	}
	nodes, edges := i.parser.ParseFile(path, content)
	if err := i.db.DeleteFile(path); err != nil {
		return err
	}
	if g != nil {
		g.RemoveFileNodes(path)
		insertFileIntoGraph(g, i.db, path, cwd, nodes, edges)
		i.ResolveReferences(g)
	}
	lang := detectLanguageForFile(filepath.Ext(path))
	return i.db.SaveFile(path, hash, lang)
}

func insertFileIntoGraph(g *Graph, db *DB, path string, cwd string, nodes []*Node, edges []*Edge) (int64, string) {
	ext := filepath.Ext(path)
	lang := detectLanguageForFile(ext)

	relDir, _ := filepath.Rel(cwd, filepath.Dir(path))
	if relDir == "." {
		relDir = ""
	}
	pkgPath := relDir
	if ext == ".go" && pkgPath == "" && len(nodes) > 0 {
		pkgPath = nodes[0].PkgPath
	}
	for _, n := range nodes {
		n.PkgPath = pkgPath
	}

	fileNode := &Node{
		Name:    filepath.Base(path),
		Kind:    KindFile,
		File:    path,
		PkgPath: pkgPath,
	}
	fileNodeID := g.AddNode(fileNode)
	if db != nil {
		_ = db.SaveNode(fileNode)
	}

	parserIDToGraphID := make(map[int64]int64)
	for idx, n := range nodes {
		n.ID = g.AddNode(n)
		parserIDToGraphID[int64(-(idx + 1))] = n.ID
		if db != nil {
			_ = db.SaveNode(n)
		}
	}

	for _, e := range edges {
		if e.FromID < 0 {
			graphID, ok := parserIDToGraphID[e.FromID]
			if !ok {
				continue
			}
			e.FromID = graphID
		}
		if e.FromID == 0 {
			if e.Kind != EdgeImports {
				continue
			}
			e.FromID = fileNodeID
		}
		e.ID = g.AddEdge(e)
		if db != nil {
			_ = db.SaveEdge(e)
		}
	}

	return fileNodeID, lang
}
