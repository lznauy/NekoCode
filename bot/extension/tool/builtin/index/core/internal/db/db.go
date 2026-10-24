package db

import (
	"context"
	"errors"
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
			_, stepErr := stmt.Step()
			return errors.Join(stepErr, stmt.Finalize())
		},
	})
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	d := &DB{pool: pool}
	if err := d.migrate(); err != nil {
		return nil, errors.Join(err, pool.Close())
	}
	return d, nil
}

func (d *DB) migrate() (err error) {
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
	if err = sqlitex.ExecScript(conn, schemaSQL); err != nil {
		return err
	}
	required := false
	err = sqlitex.ExecuteTransient(conn, "PRAGMA table_info(edges)", &sqlitex.ExecOptions{
		ResultFunc: func(stmt *sqlite.Stmt) error {
			if stmt.ColumnText(1) == "to_id" {
				required = stmt.ColumnInt(3) != 0
			}
			return nil
		},
	})
	if err != nil {
		return err
	}
	if required {
		return sqlitex.ExecScript(conn, nullableEdgeTargetSQL)
	}
	return nil
}

// Close closes the database.
func (d *DB) Close() error {
	return d.pool.Close()
}
