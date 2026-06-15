package cindex

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

var (
	ignoreDirs = map[string]bool{
		"node_modules": true, "vendor": true, "target": true, ".git": true,
		"dist": true, "build": true, "__pycache__": true, ".cache": true,
		".next": true, ".turbo": true, "coverage": true, "testdata": true,
		".nekocode": true, "venv": true, ".venv": true, "env": true,
	}

	goGeneratedRE = regexp.MustCompile(`\.(pb|mock|generated)\.go$`)

	supportedExts = map[string]bool{
		".go": true, ".js": true, ".jsx": true, ".ts": true, ".tsx": true,
		".py": true, ".rs": true,
	}
)

// ShouldSkipDir returns true if the directory should be skipped during walks.
func ShouldSkipDir(name string) bool {
	return ignoreDirs[name] || (strings.HasPrefix(name, ".") && name != ".")
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

// buildGraphFromWalk walks the directory, parses files, and builds an in-memory graph.
// If db is non-nil, nodes/edges are also persisted to the database.
func buildGraphFromWalk(cwd string, parser *Parser, db *DB) (*Graph, error) {
	g := NewGraph()

	const maxFiles = 5000
	const maxDepth = 10
	filesIndexed := 0

	err := filepath.Walk(cwd, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			name := info.Name()
			if ShouldSkipDir(name) {
				return filepath.SkipDir
			}
			rel, _ := filepath.Rel(cwd, path)
			if rel != "." && strings.Count(rel, string(filepath.Separator)) >= maxDepth {
				return filepath.SkipDir
			}
			return nil
		}
		if filesIndexed >= maxFiles {
			return filepath.SkipAll
		}

		ext := filepath.Ext(path)
		if !supportedExts[ext] {
			return nil
		}
		if ext == ".go" && goGeneratedRE.MatchString(info.Name()) {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		hash := fmt.Sprintf("%x", sha256.Sum256(content))
		nodes, edges := parser.ParseFile(path, content)

		_, lang := insertFileIntoGraph(g, db, path, cwd, nodes, edges)

		// Record file
		if db != nil {
			db.SaveFile(path, hash, lang)
		}
		g.files[path] = &FileInfo{Path: path, ContentHash: hash, Language: lang}
		filesIndexed++

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("walk: %w", err)
	}
	return g, nil
}

// LoadOrBuild loads the graph from database or builds it fresh.
func (i *Indexer) LoadOrBuild(cwd string) (*Graph, error) {
	i.mu.Lock()

	// Try loading from database
	g, err := i.db.LoadGraph()
	if err != nil {
		i.mu.Unlock()
		return nil, err
	}

	// If database is empty, build fresh
	if len(g.Nodes) == 0 {
		i.mu.Unlock()
		return i.IndexAll(cwd)
	}

	i.mu.Unlock()
	return g, nil
}

// ResolveReferences resolves cross-file call and import references.
// Exported so the Syncer can call it after incremental updates.
func (i *Indexer) ResolveReferences(g *Graph) {
	resolveReferences(g)
}

// resolveReferences resolves cross-file call and import references in the graph.
func resolveReferences(g *Graph) {
	nameIndex := make(map[string][]*Node)
	for _, n := range g.Nodes {
		nameIndex[n.Name] = append(nameIndex[n.Name], n)
	}

	for _, e := range g.Edges {
		oldToID := e.ToID

		switch e.Kind {
		case EdgeCalls:
			if e.CalleeName == "" || e.ToID != 0 {
				continue
			}
			candidates := nameIndex[e.CalleeName]
			if len(candidates) == 0 {
				continue
			}
			caller := g.Nodes[e.FromID]
			var best *Node
			for _, c := range candidates {
				if caller != nil && c.PkgPath == caller.PkgPath {
					best = c
					break
				}
			}
			if best == nil {
				best = candidates[0]
			}
			e.ToID = best.ID

		case EdgeImports:
			if e.ImportPath == "" || e.ToID != 0 {
				continue
			}
			// Match import path to PkgPath: import "myproject/handler" → PkgPath "handler"
			for _, n := range g.Nodes {
				if n.PkgPath != "" && (e.ImportPath == n.PkgPath || strings.HasSuffix(e.ImportPath, "/"+n.PkgPath)) {
					e.ToID = n.ID
					break
				}
			}
		}

		if e.ToID != oldToID {
			oldSlice := g.edgesByTo[oldToID]
			for idx, edge := range oldSlice {
				if edge.ID == e.ID {
					g.edgesByTo[oldToID] = append(oldSlice[:idx], oldSlice[idx+1:]...)
					break
				}
			}
			g.edgesByTo[e.ToID] = append(g.edgesByTo[e.ToID], e)
		}
	}
}

// extToLang maps file extensions to language names.
var extToLang = map[string]string{
	".go":  "go",
	".js":  "javascript",
	".jsx": "javascript",
	".ts":  "typescript",
	".tsx": "typescript",
	".py":  "python",
	".rs":  "rust",
	".java": "java",
}

// detectLanguageForFile returns the language name for a file extension.
func detectLanguageForFile(ext string) string {
	if lang, ok := extToLang[ext]; ok {
		return lang
	}
	return "unknown"
}

// insertFileIntoGraph adds a parsed file's nodes and edges to the graph and DB.
// Returns the file node ID and the detected language. If db is nil, DB writes
// are skipped (used during initial walk when DB is not yet open).
func insertFileIntoGraph(g *Graph, db *DB, path string, cwd string, nodes []*Node, edges []*Edge) (int64, string) {
	ext := filepath.Ext(path)
	lang := detectLanguageForFile(ext)

	// Compute package path from directory structure
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

	// Create file-level node to anchor import edges
	fileNode := &Node{
		Name:    filepath.Base(path),
		Kind:    "file",
		File:    path,
		PkgPath: pkgPath,
	}
	fileNodeID := g.AddNode(fileNode)
	if db != nil {
		db.SaveNode(fileNode)
	}

	// Add nodes to graph, tracking parser-index → graph-ID mapping
	parserIDToGraphID := make(map[int64]int64)
	for idx, n := range nodes {
		n.ID = g.AddNode(n)
		parserIDToGraphID[int64(-(idx+1))] = n.ID
		if db != nil {
			db.SaveNode(n)
		}
	}

	// Fix edge FromID and add to graph
	for _, e := range edges {
		if e.FromID < 0 {
			if graphID, ok := parserIDToGraphID[e.FromID]; ok {
				e.FromID = graphID
			} else {
				continue
			}
		}
		if e.FromID == 0 {
			if e.Kind == EdgeImports {
				e.FromID = fileNodeID
			} else {
				continue
			}
		}
		e.ID = g.AddEdge(e)
		if db != nil {
			db.SaveEdge(e)
		}
	}

	return fileNodeID, lang
}
