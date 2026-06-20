package db

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
