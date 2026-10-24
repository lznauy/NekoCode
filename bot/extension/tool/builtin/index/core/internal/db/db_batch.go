package db

import (
	"context"
	"fmt"

	graphpkg "nekocode/bot/extension/tool/builtin/index/core/internal/graph"
	"zombiezen.com/go/sqlite/sqlitex"
)

// ReplaceFile commits a file's symbols, edges and hash atomically. All writes
// share one connection and reuse its prepared statements across files.
func (d *DB) ReplaceFile(path, hash, lang string, nodes []*graphpkg.Node, edges []*graphpkg.Edge) (err error) {
	conn, err := d.pool.Take(context.Background())
	if err != nil {
		return err
	}
	defer d.pool.Put(conn)
	end, err := sqlitex.ImmediateTransaction(conn)
	if err != nil {
		return err
	}
	defer end(&err)
	if err = deleteFile(conn, path); err != nil {
		return fmt.Errorf("delete old file: %w", err)
	}
	for _, n := range nodes {
		if err = saveNode(conn, n); err != nil {
			return fmt.Errorf("save node: %w", err)
		}
	}
	for _, e := range edges {
		if err = saveEdge(conn, e); err != nil {
			return fmt.Errorf("save edge: %w", err)
		}
	}
	return saveFile(conn, path, hash, lang)
}
