package db

import (
	"context"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

type Status struct {
	Files     int
	Languages map[string]int
	LastSync  string
}

func (d *DB) RecordSync() error {
	conn, err := d.pool.Take(context.Background())
	if err != nil {
		return err
	}
	defer d.pool.Put(conn)
	return sqlitex.Execute(conn, `INSERT INTO index_metadata(key,value) VALUES ('last_sync',strftime('%Y-%m-%dT%H:%M:%fZ','now')) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, nil)
}

func (d *DB) Status() (Status, error) {
	s := Status{Languages: make(map[string]int)}
	conn, err := d.pool.Take(context.Background())
	if err != nil {
		return s, err
	}
	defer d.pool.Put(conn)
	err = sqlitex.Execute(conn, `SELECT COALESCE(language,'unknown'),COUNT(*) FROM files GROUP BY language`, &sqlitex.ExecOptions{ResultFunc: func(stmt *sqlite.Stmt) error {
		n := stmt.ColumnInt(1)
		s.Files += n
		s.Languages[stmt.ColumnText(0)] = n
		return nil
	}})
	if err != nil {
		return s, err
	}
	err = sqlitex.Execute(conn, `SELECT value FROM index_metadata WHERE key='last_sync'`, &sqlitex.ExecOptions{ResultFunc: func(stmt *sqlite.Stmt) error { s.LastSync = stmt.ColumnText(0); return nil }})
	return s, err
}
