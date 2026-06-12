package cindex

import (
	"context"
	"fmt"

	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
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
	pool *sqlitex.Pool
}

// OpenDB opens or creates a SQLite database at the given path.
func OpenDB(path string) (*DB, error) {
	pool, err := sqlitex.NewPool(path, sqlitex.PoolOptions{
		PrepareConn: func(conn *sqlite.Conn) error {
			stmt := conn.Prep("PRAGMA foreign_keys = ON;")
			_, err := stmt.Step()
			stmt.Finalize()
			return err
		},
	})
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	d := &DB{pool: pool}
	if err := d.migrate(); err != nil {
		pool.Close()
		return nil, err
	}
	return d, nil
}

func (d *DB) migrate() error {
	conn, err := d.pool.Take(context.Background())
	if err != nil {
		return err
	}
	defer d.pool.Put(conn)
	return sqlitex.ExecScript(conn, schemaSQL)
}

// Close closes the database.
func (d *DB) Close() error {
	return d.pool.Close()
}

// SaveNode inserts or updates a node.
func (d *DB) SaveNode(n *Node) error {
	conn, err := d.pool.Take(context.Background())
	if err != nil {
		return err
	}
	defer d.pool.Put(conn)
	return sqlitex.ExecuteTransient(conn, `
		INSERT INTO nodes (id, name, kind, file, line, end_line, pkg_path, signature, doc, visibility)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name=excluded.name, kind=excluded.kind, file=excluded.file,
			line=excluded.line, end_line=excluded.end_line, pkg_path=excluded.pkg_path,
			signature=excluded.signature, doc=excluded.doc, visibility=excluded.visibility
	`, &sqlitex.ExecOptions{
		Args: []any{n.ID, n.Name, string(n.Kind), n.File, n.Line, n.EndLine, n.PkgPath, n.Signature, n.Doc, n.Visibility},
	})
}

// SaveEdge inserts an edge.
func (d *DB) SaveEdge(e *Edge) error {
	conn, err := d.pool.Take(context.Background())
	if err != nil {
		return err
	}
	defer d.pool.Put(conn)
	return sqlitex.ExecuteTransient(conn, `
		INSERT INTO edges (id, from_id, to_id, kind, file, line, callee_name, import_path)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, &sqlitex.ExecOptions{
		Args: []any{e.ID, e.FromID, e.ToID, string(e.Kind), e.File, e.Line, e.CalleeName, e.ImportPath},
	})
}

// SaveFile records a file's content hash.
func (d *DB) SaveFile(path, hash, lang string) error {
	conn, err := d.pool.Take(context.Background())
	if err != nil {
		return err
	}
	defer d.pool.Put(conn)
	return sqlitex.ExecuteTransient(conn, `
		INSERT INTO files (path, content_hash, language)
		VALUES (?, ?, ?)
		ON CONFLICT(path) DO UPDATE SET content_hash=excluded.content_hash, language=excluded.language
	`, &sqlitex.ExecOptions{
		Args: []any{path, hash, lang},
	})
}

// GetFileHash returns the stored content hash for a file, or empty string if not found.
func (d *DB) GetFileHash(path string) string {
	conn, err := d.pool.Take(context.Background())
	if err != nil {
		return ""
	}
	defer d.pool.Put(conn)

	stmt, _, err := conn.PrepareTransient("SELECT content_hash FROM files WHERE path = ?;")
	if err != nil {
		return ""
	}
	defer stmt.Finalize()

	stmt.BindText(1, path)
	if hasRow, err := stmt.Step(); err != nil || !hasRow {
		return ""
	}
	return stmt.ColumnText(0)
}

// DeleteFile removes a file and all its nodes/edges.
func (d *DB) DeleteFile(path string) error {
	conn, err := d.pool.Take(context.Background())
	if err != nil {
		return err
	}
	defer d.pool.Put(conn)

	endFn, err := sqlitex.ImmediateTransaction(conn)
	if err != nil {
		return err
	}
	defer endFn(&err)

	if err := sqlitex.ExecuteTransient(conn, `
		DELETE FROM edges WHERE from_id IN (SELECT id FROM nodes WHERE file = ?)
		OR to_id IN (SELECT id FROM nodes WHERE file = ?)
	`, &sqlitex.ExecOptions{Args: []any{path, path}}); err != nil {
		return err
	}
	if err := sqlitex.ExecuteTransient(conn, "DELETE FROM nodes WHERE file = ?",
		&sqlitex.ExecOptions{Args: []any{path}}); err != nil {
		return err
	}
	return sqlitex.ExecuteTransient(conn, "DELETE FROM files WHERE path = ?",
		&sqlitex.ExecOptions{Args: []any{path}})
}

// LoadGraph loads the entire graph from the database.
func (d *DB) LoadGraph() (*Graph, error) {
	conn, err := d.pool.Take(context.Background())
	if err != nil {
		return nil, err
	}
	defer d.pool.Put(conn)

	g := NewGraph()

	// Load nodes
	stmt, _, err := conn.PrepareTransient("SELECT id, name, kind, file, line, end_line, pkg_path, signature, doc, visibility FROM nodes")
	if err != nil {
		return nil, fmt.Errorf("load nodes: %w", err)
	}
	for {
		hasRow, err := stmt.Step()
		if err != nil {
			stmt.Finalize()
			return nil, fmt.Errorf("step node: %w", err)
		}
		if !hasRow {
			break
		}
		n := &Node{
			ID:         stmt.ColumnInt64(0),
			Name:       stmt.ColumnText(1),
			Kind:       NodeKind(stmt.ColumnText(2)),
			File:       stmt.ColumnText(3),
			Line:       stmt.ColumnInt(4),
			EndLine:    stmt.ColumnInt(5),
			PkgPath:    stmt.ColumnText(6),
			Signature:  stmt.ColumnText(7),
			Doc:        stmt.ColumnText(8),
			Visibility: stmt.ColumnText(9),
		}
		g.AddNode(n)
	}
	stmt.Finalize()

	// Load edges
	edgeStmt, _, err := conn.PrepareTransient("SELECT id, from_id, to_id, kind, file, line, callee_name, import_path FROM edges")
	if err != nil {
		return nil, fmt.Errorf("load edges: %w", err)
	}
	for {
		hasRow, err := edgeStmt.Step()
		if err != nil {
			edgeStmt.Finalize()
			return nil, fmt.Errorf("step edge: %w", err)
		}
		if !hasRow {
			break
		}
		e := &Edge{
			ID:         edgeStmt.ColumnInt64(0),
			FromID:     edgeStmt.ColumnInt64(1),
			ToID:       edgeStmt.ColumnInt64(2),
			Kind:       EdgeKind(edgeStmt.ColumnText(3)),
			File:       edgeStmt.ColumnText(4),
			Line:       edgeStmt.ColumnInt(5),
			CalleeName: edgeStmt.ColumnText(6),
			ImportPath: edgeStmt.ColumnText(7),
		}
		g.AddEdge(e)
	}
	edgeStmt.Finalize()

	return g, nil
}

// Clear removes all data from the database.
func (d *DB) Clear() error {
	conn, err := d.pool.Take(context.Background())
	if err != nil {
		return err
	}
	defer d.pool.Put(conn)

	if err := sqlitex.ExecuteTransient(conn, "DELETE FROM edges", nil); err != nil {
		return err
	}
	if err := sqlitex.ExecuteTransient(conn, "DELETE FROM nodes", nil); err != nil {
		return err
	}
	return sqlitex.ExecuteTransient(conn, "DELETE FROM files", nil)
}

// FileCount returns the number of indexed files.
func (d *DB) FileCount() int {
	conn, err := d.pool.Take(context.Background())
	if err != nil {
		return 0
	}
	defer d.pool.Put(conn)

	stmt, _, err := conn.PrepareTransient("SELECT COUNT(*) FROM files")
	if err != nil {
		return 0
	}
	defer stmt.Finalize()

	if hasRow, _ := stmt.Step(); hasRow {
		return stmt.ColumnInt(0)
	}
	return 0
}

// NodeCount returns the number of indexed nodes.
func (d *DB) NodeCount() int {
	conn, err := d.pool.Take(context.Background())
	if err != nil {
		return 0
	}
	defer d.pool.Put(conn)

	stmt, _, err := conn.PrepareTransient("SELECT COUNT(*) FROM nodes")
	if err != nil {
		return 0
	}
	defer stmt.Finalize()

	if hasRow, _ := stmt.Step(); hasRow {
		return stmt.ColumnInt(0)
	}
	return 0
}

// SearchFTS performs a full-text search on node names, signatures, and docs.
func (d *DB) SearchFTS(query string, limit int) ([]*Node, error) {
	if limit <= 0 {
		limit = 20
	}
	conn, err := d.pool.Take(context.Background())
	if err != nil {
		return nil, err
	}
	defer d.pool.Put(conn)

	stmt, _, err := conn.PrepareTransient(`
		SELECT n.id, n.name, n.kind, n.file, n.line, n.end_line, n.pkg_path, n.signature, n.doc, n.visibility
		FROM nodes_fts fts
		JOIN nodes n ON n.id = fts.rowid
		WHERE nodes_fts MATCH ?
		ORDER BY rank
		LIMIT ?
	`)
	if err != nil {
		return nil, fmt.Errorf("fts search: %w", err)
	}
	defer stmt.Finalize()

	stmt.BindText(1, query)
	stmt.BindInt64(2, int64(limit))

	var nodes []*Node
	for {
		hasRow, err := stmt.Step()
		if err != nil {
			return nil, fmt.Errorf("fts step: %w", err)
		}
		if !hasRow {
			break
		}
		n := &Node{
			ID:         stmt.ColumnInt64(0),
			Name:       stmt.ColumnText(1),
			Kind:       NodeKind(stmt.ColumnText(2)),
			File:       stmt.ColumnText(3),
			Line:       stmt.ColumnInt(4),
			EndLine:    stmt.ColumnInt(5),
			PkgPath:    stmt.ColumnText(6),
			Signature:  stmt.ColumnText(7),
			Doc:        stmt.ColumnText(8),
			Visibility: stmt.ColumnText(9),
		}
		nodes = append(nodes, n)
	}
	return nodes, nil
}
