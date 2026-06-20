package service

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	graphpkg "nekocode/bot/index/graph"
	indexerpkg "nekocode/bot/index/indexer"
	syncerpkg "nekocode/bot/index/syncer"
)

// projectMarkers are files/directories that indicate a project root.
var projectMarkers = []string{
	".git",
	"go.mod",
	"package.json",
	"Cargo.toml",
	"pyproject.toml",
	"setup.py",
	"pom.xml",
	"build.gradle",
	".svn",
	".hg",
}

// Manager is the main entry point for the code graph system.
// It replaces the old projctx.ProjectIndex with a full code graph.
type Manager struct {
	mu      sync.RWMutex
	indexer *indexerpkg.Indexer
	syncer  *syncerpkg.Syncer
	graph   *graphpkg.Graph
	cwd     string
	root    string // project root directory (may differ from cwd)
}

// NewManager creates a new code graph manager.
// It walks up from cwd to find a project root (by looking for .git, go.mod, etc.).
// If no project root is found, the returned Manager has a nil indexer and Init() is a no-op.
func NewManager(cwd string) (*Manager, error) {
	root := findProjectRoot(cwd)
	if root == "" {
		// Not a project directory — skip index entirely
		return &Manager{cwd: cwd}, nil
	}

	nekocodeDir := filepath.Join(root, ".nekocode")
	if err := os.MkdirAll(nekocodeDir, 0o755); err != nil {
		return nil, fmt.Errorf("create .nekocode dir: %w", err)
	}

	dbPath := filepath.Join(nekocodeDir, "index.db")
	indexer, err := indexerpkg.NewIndexer(dbPath)
	if err != nil {
		return nil, fmt.Errorf("open indexer: %w", err)
	}

	return &Manager{
		indexer: indexer,
		cwd:     cwd,
		root:    root,
	}, nil
}

// Init initializes the code graph by loading from database or building fresh.
// If no project root was found (indexer is nil), this is a no-op.
func (m *Manager) Init() error {
	if m.indexer == nil {
		return nil
	}

	graph, err := m.indexer.LoadOrBuild(m.root)
	if err != nil {
		return fmt.Errorf("load or build: %w", err)
	}
	m.mu.Lock()
	m.graph = graph
	m.mu.Unlock()

	// Start file syncer
	syncer, err := syncerpkg.NewSyncer(m.indexer, m.root, &m.mu)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not start file syncer: %v\n", err)
	} else {
		m.syncer = syncer
		syncer.SetGraph(graph)
		syncer.Start()
	}

	return nil
}

// Graph returns the current code graph.
func (m *Manager) Graph() *graphpkg.Graph {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.graph
}

func (m *Manager) Query(fn func(*graphpkg.Graph) string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.graph == nil {
		return "Project index not available for this workspace."
	}
	return fn(m.graph)
}

// Indexer returns the indexer.
func (m *Manager) Indexer() *indexerpkg.Indexer {
	return m.indexer
}

// CWD returns the working directory used for display-relative index formatting.
func (m *Manager) CWD() string {
	return m.cwd
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
	if m.indexer == nil {
		return nil
	}

	graph, err := m.indexer.IndexAll(m.root)
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

// findProjectRoot walks up from dir looking for project marker files.
// Returns the directory containing the marker, or "" if none found.
func findProjectRoot(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return ""
	}

	for {
		for _, marker := range projectMarkers {
			if _, err := os.Stat(filepath.Join(abs, marker)); err == nil {
				return abs
			}
		}

		parent := filepath.Dir(abs)
		if parent == abs {
			// Reached filesystem root
			return ""
		}
		abs = parent
	}
}
