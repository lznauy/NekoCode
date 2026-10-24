package indexer

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	graphpkg "nekocode/bot/extension/tool/builtin/index/core/internal/graph"
)

// LoadOrBuild loads the graph from database or builds it fresh.
func (i *Indexer) LoadOrBuild(cwd string) (*graphpkg.Graph, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	g, err := i.db.LoadGraph()
	if err != nil {
		return nil, err
	}
	if len(g.Nodes) == 0 {
		return i.indexAllLocked(cwd)
	}
	indexed, err := i.db.LoadFileHashes()
	if err != nil {
		return nil, err
	}
	current, err := currentFileHashes(cwd)
	if err != nil {
		return nil, err
	}
	var changes []FileChange
	for path := range indexed {
		if _, ok := current[path]; !ok {
			changes = append(changes, FileChange{Path: path, Deleted: true})
		}
	}
	for path, hash := range current {
		if indexed[path] == hash {
			continue
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read changed file: %w", err)
		}
		changes = append(changes, FileChange{Path: path, Content: content})
	}
	sort.Slice(changes, func(a, b int) bool { return changes[a].Path < changes[b].Path })
	if len(changes) > 0 {
		if err := i.applyChangesLocked(g, cwd, changes); err != nil {
			return nil, err
		}
	} else {
		resolveReferences(g)
	}
	return g, nil
}

func currentFileHashes(cwd string) (map[string]string, error) {
	hashes := make(map[string]string)
	filesSeen := 0
	err := filepath.Walk(cwd, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
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
		if filesSeen >= indexMaxFiles {
			return filepath.SkipAll
		}
		if !SupportsFile(path) {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		hashes[path] = fmt.Sprintf("%x", sha256.Sum256(content))
		filesSeen++
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk: %w", err)
	}
	return hashes, nil
}
