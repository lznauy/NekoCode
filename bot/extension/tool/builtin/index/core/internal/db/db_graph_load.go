package db

import (
	"context"
	"fmt"

	graphpkg "nekocode/bot/extension/tool/builtin/index/core/internal/graph"

	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

// LoadGraph loads the entire graph from the database.
func (d *DB) LoadGraph() (*graphpkg.Graph, error) {
	conn, err := d.pool.Take(context.Background())
	if err != nil {
		return nil, err
	}
	defer d.pool.Put(conn)

	g := graphpkg.NewGraph()

	stmt, _, err := conn.PrepareTransient("SELECT id, name, kind, file, line, end_line, pkg_path, signature, doc, visibility FROM nodes")
	if err != nil {
		return nil, fmt.Errorf("load nodes: %w", err)
	}
	for {
		hasRow, err := stmt.Step()
		if err != nil {
			_ = stmt.Finalize()
			return nil, fmt.Errorf("step node: %w", err)
		}
		if !hasRow {
			break
		}
		g.AddNode(&graphpkg.Node{
			ID:         stmt.ColumnInt64(0),
			Name:       stmt.ColumnText(1),
			Kind:       graphpkg.NodeKind(stmt.ColumnText(2)),
			File:       stmt.ColumnText(3),
			Line:       stmt.ColumnInt(4),
			EndLine:    stmt.ColumnInt(5),
			PkgPath:    stmt.ColumnText(6),
			Signature:  stmt.ColumnText(7),
			Doc:        stmt.ColumnText(8),
			Visibility: stmt.ColumnText(9),
		})
	}
	if err := stmt.Finalize(); err != nil {
		return nil, fmt.Errorf("finalize nodes: %w", err)
	}

	edgeStmt, _, err := conn.PrepareTransient("SELECT id, from_id, to_id, kind, file, line, callee_name, import_path FROM edges")
	if err != nil {
		return nil, fmt.Errorf("load edges: %w", err)
	}
	for {
		hasRow, err := edgeStmt.Step()
		if err != nil {
			_ = edgeStmt.Finalize()
			return nil, fmt.Errorf("step edge: %w", err)
		}
		if !hasRow {
			break
		}
		g.AddEdge(&graphpkg.Edge{
			ID:         edgeStmt.ColumnInt64(0),
			FromID:     edgeStmt.ColumnInt64(1),
			ToID:       edgeStmt.ColumnInt64(2),
			Kind:       graphpkg.EdgeKind(edgeStmt.ColumnText(3)),
			File:       edgeStmt.ColumnText(4),
			Line:       edgeStmt.ColumnInt(5),
			CalleeName: edgeStmt.ColumnText(6),
			ImportPath: edgeStmt.ColumnText(7),
		})
	}
	if err := edgeStmt.Finalize(); err != nil {
		return nil, fmt.Errorf("finalize edges: %w", err)
	}

	err = sqlitex.ExecuteTransient(conn, "SELECT path, content_hash, language FROM files", &sqlitex.ExecOptions{
		ResultFunc: func(stmt *sqlite.Stmt) error {
			g.SetFileInfo(stmt.ColumnText(0), stmt.ColumnText(1), stmt.ColumnText(2))
			return nil
		},
	})
	if err != nil {
		return nil, fmt.Errorf("load files: %w", err)
	}
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
