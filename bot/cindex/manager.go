package cindex

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Manager is the main entry point for the code graph system.
// It replaces the old projctx.ProjectIndex with a full code graph.
type Manager struct {
	mu      sync.RWMutex
	indexer *Indexer
	syncer  *Syncer
	graph   *Graph
	cwd     string
}

// NewManager creates a new code graph manager.
// If the database cannot be opened (e.g. no FTS5 support), it falls back to
// an in-memory-only mode where the graph is rebuilt on every Init().
func NewManager(cwd string) (*Manager, error) {
	// Ensure .nekocode directory exists
	nekocodeDir := filepath.Join(cwd, ".nekocode")
	if err := os.MkdirAll(nekocodeDir, 0o755); err != nil {
		return nil, fmt.Errorf("create .nekocode dir: %w", err)
	}

	// Create database path
	dbPath := filepath.Join(nekocodeDir, "cindex.db")

	// Create indexer — may fail if FTS5 is unavailable
	indexer, err := NewIndexer(dbPath)
	if err != nil {
		// Fallback: create manager without DB (memory-only)
		return &Manager{cwd: cwd}, nil
	}

	m := &Manager{
		indexer: indexer,
		cwd:     cwd,
	}

	return m, nil
}

// Init initializes the code graph by loading from database or building fresh.
func (m *Manager) Init() error {
	if m.indexer != nil {
		// DB mode: try loading from database first
		graph, err := m.indexer.LoadOrBuild(m.cwd)
		if err != nil {
			return fmt.Errorf("load or build: %w", err)
		}
		m.mu.Lock()
		m.graph = graph
		m.mu.Unlock()

		// Start file syncer
		syncer, err := NewSyncer(m.indexer, m.cwd)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not start file syncer: %v\n", err)
		} else {
			m.syncer = syncer
			syncer.SetGraph(graph)
			syncer.Start()
		}
	} else {
		// Memory-only mode: build graph without DB persistence
		graph, err := buildGraphFromDir(m.cwd)
		if err != nil {
			return fmt.Errorf("build graph: %w", err)
		}
		m.mu.Lock()
		m.graph = graph
		m.mu.Unlock()
	}

	return nil
}

// Graph returns the current code graph.
func (m *Manager) Graph() *Graph {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.graph
}

// Indexer returns the indexer.
func (m *Manager) Indexer() *Indexer {
	return m.indexer
}

// Close closes the manager and releases resources.
func (m *Manager) Close() error {
	if m.syncer != nil {
		m.syncer.Stop()
	}
	if m.indexer != nil {
		return m.indexer.Close()
	}
	return nil
}

// Rebuild forces a full re-index of the project.
func (m *Manager) Rebuild() error {
	var graph *Graph
	var err error

	if m.indexer != nil {
		graph, err = m.indexer.IndexAll(m.cwd)
	} else {
		graph, err = buildGraphFromDir(m.cwd)
	}
	if err != nil {
		return fmt.Errorf("rebuild: %w", err)
	}

	m.mu.Lock()
	m.graph = graph
	m.mu.Unlock()

	if m.syncer != nil {
		m.syncer.SetGraph(graph)
	}

	return nil
}

// buildGraphFromDir builds an in-memory graph without DB persistence.
func buildGraphFromDir(cwd string) (*Graph, error) {
	g, err := buildGraphFromWalk(cwd, NewParser(), nil)
	if err != nil {
		return nil, err
	}
	resolveReferences(g)
	return g, nil
}
