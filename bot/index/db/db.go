package db

import (
	"context"
	"fmt"

	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

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
