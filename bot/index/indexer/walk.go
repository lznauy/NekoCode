package indexer

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// IndexAll indexes all supported files in the given directory.
func (i *Indexer) IndexAll(cwd string) (*Graph, error) {
	i.mu.Lock()
	defer i.mu.Unlock()

	if err := i.db.Clear(); err != nil {
		return nil, fmt.Errorf("clear db: %w", err)
	}

	g, err := buildGraphFromWalk(cwd, i.parser, i.db)
	if err != nil {
		return nil, err
	}
	resolveReferences(g)
	return g, nil
}

func buildGraphFromWalk(cwd string, parser *Parser, db *DB) (*Graph, error) {
	g := NewGraph()
	filesIndexed := 0

	err := filepath.Walk(cwd, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if ShouldSkipDir(info.Name()) {
				return filepath.SkipDir
			}
			rel, _ := filepath.Rel(cwd, path)
			if rel != "." && strings.Count(rel, string(filepath.Separator)) >= indexMaxDepth {
				return filepath.SkipDir
			}
			return nil
		}
		if filesIndexed >= indexMaxFiles {
			return filepath.SkipAll
		}
		if !SupportsFile(path) {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		hash := fmt.Sprintf("%x", sha256.Sum256(content))
		nodes, edges := parser.ParseFile(path, content)
		_, lang := insertFileIntoGraph(g, db, path, cwd, nodes, edges)
		if db != nil {
			_ = db.SaveFile(path, hash, lang)
		}
		g.SetFileInfo(path, hash, lang)
		filesIndexed++
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk: %w", err)
	}
	return g, nil
}
