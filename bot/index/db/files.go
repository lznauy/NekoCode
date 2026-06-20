package db

import (
	"context"

	"zombiezen.com/go/sqlite/sqlitex"
)

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

// GetFileHash returns the stored content hash for a file.
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

// LoadFileHashes returns the indexed file hash map from the database.
func (d *DB) LoadFileHashes() (map[string]string, error) {
	conn, err := d.pool.Take(context.Background())
	if err != nil {
		return nil, err
	}
	defer d.pool.Put(conn)

	stmt, _, err := conn.PrepareTransient("SELECT path, content_hash FROM files;")
	if err != nil {
		return nil, err
	}
	defer stmt.Finalize()

	out := make(map[string]string)
	for {
		hasRow, err := stmt.Step()
		if err != nil {
			return nil, err
		}
		if !hasRow {
			break
		}
		out[stmt.ColumnText(0)] = stmt.ColumnText(1)
	}
	return out, nil
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
