package indexer

import (
	"fmt"
	"sync"

	dbpkg "nekocode/bot/index/db"
	graphpkg "nekocode/bot/index/graph"
	parserpkg "nekocode/bot/index/parser"
)

type DB = dbpkg.DB
type Graph = graphpkg.Graph
type Node = graphpkg.Node
type Edge = graphpkg.Edge
type Parser = parserpkg.Parser

const (
	KindFunc    = graphpkg.KindFunc
	KindFile    = graphpkg.KindFile
	EdgeCalls   = graphpkg.EdgeCalls
	EdgeImports = graphpkg.EdgeImports
)

func OpenDB(path string) (*DB, error) {
	return dbpkg.OpenDB(path)
}

func NewGraph() *Graph {
	return graphpkg.NewGraph()
}

func NewParser() *Parser {
	return parserpkg.NewParser()
}

// Indexer orchestrates the indexing process.
type Indexer struct {
	parser *Parser
	db     *DB
	mu     sync.Mutex
}

// NewIndexer creates a new indexer.
func NewIndexer(dbPath string) (*Indexer, error) {
	db, err := OpenDB(dbPath)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	return &Indexer{
		parser: NewParser(),
		db:     db,
	}, nil
}

// Close closes the indexer and database.
func (i *Indexer) Close() error {
	return i.db.Close()
}
