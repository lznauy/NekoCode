package indexer

import (
	"fmt"
	"sort"
	"sync"

	dbpkg "nekocode/bot/extension/tool/builtin/index/core/internal/db"
	parserpkg "nekocode/bot/extension/tool/builtin/index/core/internal/parser"
)

// Indexer orchestrates the indexing process.
type Indexer struct {
	parser *parserpkg.Parser
	db     *dbpkg.DB
	mu     sync.Mutex
}

// NewIndexer creates a new indexer.
func NewIndexer(dbPath string) (*Indexer, error) {
	db, err := dbpkg.OpenDB(dbPath)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	return &Indexer{
		parser: parserpkg.NewParser(),
		db:     db,
	}, nil
}

// Close closes the indexer and database.
func (i *Indexer) Close() error {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.parser.Close()
	return i.db.Close()
}

func (i *Indexer) RecordSync() error { return i.db.RecordSync() }

// IndexedFilePaths includes metadata-only files, even when no graph is loaded.
func (i *Indexer) IndexedFilePaths() ([]string, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	hashes, err := i.db.LoadFileHashes()
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(hashes))
	for path := range hashes {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths, nil
}

func (i *Indexer) Status() (dbpkg.Status, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.db.Status()
}
