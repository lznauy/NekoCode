package db

import (
	"context"
	"fmt"

	graphpkg "nekocode/bot/index/graph"
)

// SearchFTS performs a full-text search on node names, signatures, and docs.
func (d *DB) SearchFTS(query string, limit int) ([]*graphpkg.Node, error) {
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

	var nodes []*graphpkg.Node
	for {
		hasRow, err := stmt.Step()
		if err != nil {
			return nil, fmt.Errorf("fts step: %w", err)
		}
		if !hasRow {
			break
		}
		nodes = append(nodes, &graphpkg.Node{
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
	return nodes, nil
}
