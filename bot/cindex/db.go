package cindex

import (
	"database/sql"
	"fmt"

	_ "github.com/mattn/go-sqlite3"
)

const schemaSQL = `
CREATE TABLE IF NOT EXISTS nodes (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    kind TEXT NOT NULL,
    file TEXT NOT NULL,
    line INTEGER NOT NULL,
    end_line INTEGER,
    pkg_path TEXT,
    signature TEXT,
    doc TEXT,
    visibility TEXT,
    content_hash TEXT
);

CREATE TABLE IF NOT EXISTS edges (
    id INTEGER PRIMARY KEY,
    from_id INTEGER NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    to_id INTEGER NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    kind TEXT NOT NULL,
    file TEXT,
    line INTEGER,
    callee_name TEXT,
    import_path TEXT
);

CREATE TABLE IF NOT EXISTS files (
    path TEXT PRIMARY KEY,
    content_hash TEXT NOT NULL,
    language TEXT,
    indexed_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE VIRTUAL TABLE IF NOT EXISTS nodes_fts USING fts5(
    name, signature, doc,
    content=nodes,
    content_rowid=id
);

-- Triggers to keep FTS index in sync with nodes table
CREATE TRIGGER IF NOT EXISTS nodes_ai AFTER INSERT ON nodes BEGIN
    INSERT INTO nodes_fts(rowid, name, signature, doc)
    VALUES (new.id, new.name, new.signature, new.doc);
END;

CREATE TRIGGER IF NOT EXISTS nodes_ad AFTER DELETE ON nodes BEGIN
    INSERT INTO nodes_fts(nodes_fts, rowid, name, signature, doc)
    VALUES ('delete', old.id, old.name, old.signature, old.doc);
END;

CREATE TRIGGER IF NOT EXISTS nodes_au AFTER UPDATE ON nodes BEGIN
    INSERT INTO nodes_fts(nodes_fts, rowid, name, signature, doc)
    VALUES ('delete', old.id, old.name, old.signature, old.doc);
    INSERT INTO nodes_fts(rowid, name, signature, doc)
    VALUES (new.id, new.name, new.signature, new.doc);
END;

CREATE INDEX IF NOT EXISTS idx_nodes_name ON nodes(name);
CREATE INDEX IF NOT EXISTS idx_nodes_file ON nodes(file);
CREATE INDEX IF NOT EXISTS idx_nodes_kind ON nodes(kind);
CREATE INDEX IF NOT EXISTS idx_edges_from ON edges(from_id);
CREATE INDEX IF NOT EXISTS idx_edges_to ON edges(to_id);
CREATE INDEX IF NOT EXISTS idx_edges_kind ON edges(kind);
`

// DB wraps SQLite operations for the code graph.
type DB struct {
	db *sql.DB
}

// OpenDB opens or creates a SQLite database at the given path.
func OpenDB(path string) (*DB, error) {
	db, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_foreign_keys=ON")
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	d := &DB{db: db}
	if err := d.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return d, nil
}

func (d *DB) migrate() error {
	_, err := d.db.Exec(schemaSQL)
	return err
}

// Close closes the database.
func (d *DB) Close() error {
	return d.db.Close()
}

// SaveNode inserts or updates a node.
func (d *DB) SaveNode(n *Node) error {
	_, err := d.db.Exec(`
		INSERT INTO nodes (id, name, kind, file, line, end_line, pkg_path, signature, doc, visibility)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name=excluded.name, kind=excluded.kind, file=excluded.file,
			line=excluded.line, end_line=excluded.end_line, pkg_path=excluded.pkg_path,
			signature=excluded.signature, doc=excluded.doc, visibility=excluded.visibility
	`, n.ID, n.Name, string(n.Kind), n.File, n.Line, n.EndLine, n.PkgPath, n.Signature, n.Doc, n.Visibility)
	return err
}

// SaveEdge inserts an edge.
func (d *DB) SaveEdge(e *Edge) error {
	_, err := d.db.Exec(`
		INSERT INTO edges (id, from_id, to_id, kind, file, line, callee_name, import_path)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, e.ID, e.FromID, e.ToID, string(e.Kind), e.File, e.Line, e.CalleeName, e.ImportPath)
	return err
}

// SaveFile records a file's content hash.
func (d *DB) SaveFile(path, hash, lang string) error {
	_, err := d.db.Exec(`
		INSERT INTO files (path, content_hash, language)
		VALUES (?, ?, ?)
		ON CONFLICT(path) DO UPDATE SET content_hash=excluded.content_hash, language=excluded.language
	`, path, hash, lang)
	return err
}

// GetFileHash returns the stored content hash for a file, or empty string if not found.
func (d *DB) GetFileHash(path string) string {
	var hash string
	d.db.QueryRow("SELECT content_hash FROM files WHERE path = ?", path).Scan(&hash)
	return hash
}

// DeleteFile removes a file and all its nodes/edges.
func (d *DB) DeleteFile(path string) error {
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Delete edges referencing nodes in this file
	_, err = tx.Exec(`
		DELETE FROM edges WHERE from_id IN (SELECT id FROM nodes WHERE file = ?)
		OR to_id IN (SELECT id FROM nodes WHERE file = ?)
	`, path, path)
	if err != nil {
		return err
	}

	// Delete nodes
	_, err = tx.Exec("DELETE FROM nodes WHERE file = ?", path)
	if err != nil {
		return err
	}

	// Delete file record
	_, err = tx.Exec("DELETE FROM files WHERE path = ?", path)
	if err != nil {
		return err
	}

	return tx.Commit()
}

// LoadGraph loads the entire graph from the database.
func (d *DB) LoadGraph() (*Graph, error) {
	g := NewGraph()

	// Load nodes
	rows, err := d.db.Query("SELECT id, name, kind, file, line, end_line, pkg_path, signature, doc, visibility FROM nodes")
	if err != nil {
		return nil, fmt.Errorf("load nodes: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var n Node
		var kind string
		err := rows.Scan(&n.ID, &n.Name, &kind, &n.File, &n.Line, &n.EndLine, &n.PkgPath, &n.Signature, &n.Doc, &n.Visibility)
		if err != nil {
			return nil, fmt.Errorf("scan node: %w", err)
		}
		n.Kind = NodeKind(kind)
		g.AddNode(&n)
	}

	// Load edges
	edgeRows, err := d.db.Query("SELECT id, from_id, to_id, kind, file, line, callee_name, import_path FROM edges")
	if err != nil {
		return nil, fmt.Errorf("load edges: %w", err)
	}
	defer edgeRows.Close()

	for edgeRows.Next() {
		var e Edge
		var kind string
		err := edgeRows.Scan(&e.ID, &e.FromID, &e.ToID, &kind, &e.File, &e.Line, &e.CalleeName, &e.ImportPath)
		if err != nil {
			return nil, fmt.Errorf("scan edge: %w", err)
		}
		e.Kind = EdgeKind(kind)
		g.AddEdge(&e)
	}

	return g, nil
}

// Clear removes all data from the database.
func (d *DB) Clear() error {
	_, err := d.db.Exec("DELETE FROM edges")
	if err != nil {
		return err
	}
	_, err = d.db.Exec("DELETE FROM nodes")
	if err != nil {
		return err
	}
	_, err = d.db.Exec("DELETE FROM files")
	return err
}

// FileCount returns the number of indexed files.
func (d *DB) FileCount() int {
	var count int
	d.db.QueryRow("SELECT COUNT(*) FROM files").Scan(&count)
	return count
}

// NodeCount returns the number of indexed nodes.
func (d *DB) NodeCount() int {
	var count int
	d.db.QueryRow("SELECT COUNT(*) FROM nodes").Scan(&count)
	return count
}

// SearchFTS performs a full-text search on node names, signatures, and docs.
func (d *DB) SearchFTS(query string, limit int) ([]*Node, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := d.db.Query(`
		SELECT n.id, n.name, n.kind, n.file, n.line, n.end_line, n.pkg_path, n.signature, n.doc, n.visibility
		FROM nodes_fts fts
		JOIN nodes n ON n.id = fts.rowid
		WHERE nodes_fts MATCH ?
		ORDER BY rank
		LIMIT ?
	`, query, limit)
	if err != nil {
		return nil, fmt.Errorf("fts search: %w", err)
	}
	defer rows.Close()

	var nodes []*Node
	for rows.Next() {
		var n Node
		var kind string
		if err := rows.Scan(&n.ID, &n.Name, &kind, &n.File, &n.Line, &n.EndLine, &n.PkgPath, &n.Signature, &n.Doc, &n.Visibility); err != nil {
			continue
		}
		n.Kind = NodeKind(kind)
		nodes = append(nodes, &n)
	}
	return nodes, nil
}
