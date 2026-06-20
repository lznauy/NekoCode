package db

import (
	"context"

	graphpkg "nekocode/bot/index/graph"

	"zombiezen.com/go/sqlite/sqlitex"
)

// SaveNode inserts or updates a node.
func (d *DB) SaveNode(n *graphpkg.Node) error {
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
func (d *DB) SaveEdge(e *graphpkg.Edge) error {
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
