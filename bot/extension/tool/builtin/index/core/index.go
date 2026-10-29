package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	graphpkg "nekocode/bot/extension/tool/builtin/index/core/internal/graph"
	indexerpkg "nekocode/bot/extension/tool/builtin/index/core/internal/indexer"
	syncerpkg "nekocode/bot/extension/tool/builtin/index/core/internal/syncer"
)

type Symbol struct {
	Name    string
	Kind    string
	File    string
	Line    int
	PkgPath string
}

type File struct {
	Path string
}

type SearchResult struct {
	Name string
	Kind string
	File string
	Line int
}

// CallRelation describes one resolved caller/callee relationship. Symbol is
// the symbol matched by the query; Related is its caller or callee.
type CallRelation struct {
	Symbol  Symbol
	Related Symbol
}

// Manager owns one workspace code index.
type Manager struct {
	mu               sync.RWMutex
	indexer          *indexerpkg.Indexer
	syncer           *syncerpkg.Syncer
	graph            *graphpkg.Graph
	cwd              string
	root             string
	lease            *indexLease
	closeOnce        sync.Once
	closeErr         error
	databaseCloseErr error
	leaseOnce        sync.Once
	leaseErr         error
}

func NewManager(cwd string) (*Manager, error) {
	root := findProjectRoot(cwd)
	if root == "" {
		return &Manager{cwd: cwd}, nil
	}

	lease, err := acquireIndexLease(root, false)
	if err != nil {
		return nil, err
	}
	manager, err := newManagerWithLease(cwd, root, lease)
	if err != nil {
		_ = lease.close()
	}
	return manager, err
}

func newManagerWithLease(cwd, root string, lease *indexLease) (*Manager, error) {
	idx, err := indexerpkg.NewIndexer(filepath.Join(root, ".nekocode", "index.db"))
	if err != nil {
		return nil, fmt.Errorf("open indexer: %w", err)
	}
	return &Manager{indexer: idx, cwd: cwd, root: root, lease: lease}, nil
}

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
	if err := m.indexer.RecordSync(); err != nil {
		return err
	}

	fileSyncer, err := syncerpkg.NewSyncer(m.indexer, m.root, &m.mu)
	m.syncer = fileSyncer
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not start file syncer: %v\n", err)
		return nil
	}
	fileSyncer.SetGraph(graph)
	fileSyncer.Start()
	return nil
}

func (m *Manager) Close() error {
	return m.close(true)
}

func (m *Manager) close(releaseLease bool) error {
	m.closeOnce.Do(func() {
		var syncErr error
		if m.syncer != nil {
			syncErr = m.syncer.Stop()
		}
		if m.indexer != nil {
			m.databaseCloseErr = m.indexer.Close()
		}
		m.closeErr = errors.Join(syncErr, m.databaseCloseErr)
	})
	var leaseErr error
	// Do not release database ownership if SQLite could not be closed.
	if releaseLease && m.databaseCloseErr == nil {
		leaseErr = m.releaseLease()
	}
	return errors.Join(m.closeErr, leaseErr)
}

func (m *Manager) releaseLease() error {
	m.leaseOnce.Do(func() {
		m.leaseErr = m.lease.close()
	})
	return m.leaseErr
}

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

func (m *Manager) Skeleton() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.graph == nil {
		return "Project index not available for this workspace."
	}
	return m.graph.FormatSkeleton(m.cwd)
}

func (m *Manager) QuerySymbol(name string) []Symbol {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.graph == nil {
		return nil
	}
	symbols := m.graph.QuerySymbol(name)
	out := make([]Symbol, 0, len(symbols))
	for _, s := range symbols {
		out = append(out, Symbol{
			Name:    s.Name,
			Kind:    s.Kind,
			File:    s.File,
			Line:    s.Line,
			PkgPath: s.PkgPath,
		})
	}
	return out
}

// QueryCallers returns the distinct symbols that call symbols matching name.
func (m *Manager) QueryCallers(name string) []CallRelation {
	return m.queryCalls(name, true)
}

// QueryCallees returns the distinct symbols called by symbols matching name.
func (m *Manager) QueryCallees(name string) []CallRelation {
	return m.queryCalls(name, false)
}

func (m *Manager) queryCalls(name string, callers bool) []CallRelation {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.graph == nil {
		return nil
	}

	var out []CallRelation
	seen := make(map[[2]int64]bool)
	for _, node := range m.graph.FindNodesByName(name) {
		related := m.graph.GetCallees(node.ID)
		if callers {
			related = m.graph.GetCallers(node.ID)
		}
		for _, other := range related {
			key := [2]int64{node.ID, other.ID}
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, CallRelation{
				Symbol:  symbolFromNode(node),
				Related: symbolFromNode(other),
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		left, right := out[i], out[j]
		if left.Symbol.File != right.Symbol.File {
			return left.Symbol.File < right.Symbol.File
		}
		if left.Symbol.Line != right.Symbol.Line {
			return left.Symbol.Line < right.Symbol.Line
		}
		if left.Symbol.Name != right.Symbol.Name {
			return left.Symbol.Name < right.Symbol.Name
		}
		if left.Related.File != right.Related.File {
			return left.Related.File < right.Related.File
		}
		if left.Related.Line != right.Related.Line {
			return left.Related.Line < right.Related.Line
		}
		return left.Related.Name < right.Related.Name
	})
	return out
}

func symbolFromNode(node *graphpkg.Node) Symbol {
	return Symbol{
		Name: node.Name, Kind: string(node.Kind), File: node.File,
		Line: node.Line, PkgPath: node.PkgPath,
	}
}

func (m *Manager) QueryDeps(pkgPath string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.graph == nil {
		return nil
	}
	return m.graph.QueryDeps(pkgPath)
}

func (m *Manager) QueryFile(name string) []File {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.graph == nil {
		return nil
	}
	files := m.graph.QueryFile(name)
	out := make([]File, 0, len(files))
	for _, f := range files {
		out = append(out, File{Path: f.Path})
	}
	return out
}

func (m *Manager) Search(term string, limit int) ([]SearchResult, error) {
	if m.indexer == nil {
		return nil, nil
	}
	nodes, err := m.indexer.SearchFTS(term, limit)
	if err != nil {
		return nil, err
	}
	out := make([]SearchResult, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, SearchResult{
			Name: n.Name,
			Kind: string(n.Kind),
			File: n.File,
			Line: n.Line,
		})
	}
	return out, nil
}

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
			return ""
		}
		abs = parent
	}
}
