package indexer

import (
	"fmt"
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
