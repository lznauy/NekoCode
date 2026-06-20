package indexer

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// LoadOrBuild loads the graph from database or builds it fresh.
func (i *Indexer) LoadOrBuild(cwd string) (*Graph, error) {
	i.mu.Lock()

	g, err := i.db.LoadGraph()
	if err != nil {
		i.mu.Unlock()
		return nil, err
	}

	stale, err := i.isStale(cwd)
	if err != nil {
		i.mu.Unlock()
		return nil, err
	}
	if len(g.Nodes) == 0 || stale {
		i.mu.Unlock()
		return i.IndexAll(cwd)
	}

	i.mu.Unlock()
	return g, nil
}

func (i *Indexer) isStale(cwd string) (bool, error) {
	indexed, err := i.db.LoadFileHashes()
	if err != nil {
		return false, err
	}
	current, err := currentFileHashes(cwd)
	if err != nil {
		return false, err
	}
	if len(indexed) != len(current) {
		return true, nil
	}
	for path, hash := range current {
		if indexed[path] != hash {
			return true, nil
		}
	}
	return false, nil
}

func currentFileHashes(cwd string) (map[string]string, error) {
	hashes := make(map[string]string)
	filesSeen := 0
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
		if filesSeen >= indexMaxFiles {
			return filepath.SkipAll
		}
		if !SupportsFile(path) {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return nil
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
