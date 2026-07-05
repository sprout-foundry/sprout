package codegraph

// schemaSQL creates all tables and indexes for the code graph store.
const schemaSQL = `
CREATE TABLE IF NOT EXISTS nodes (
    id INTEGER PRIMARY KEY,
    qualified_name TEXT NOT NULL,
    display_name TEXT NOT NULL,
    file_path TEXT NOT NULL,
    line INTEGER NOT NULL DEFAULT 0,
    kind TEXT NOT NULL,
    language TEXT NOT NULL DEFAULT '',
    file_mtime TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_nodes_display_name ON nodes(display_name);
CREATE UNIQUE INDEX IF NOT EXISTS idx_nodes_qualified_name ON nodes(qualified_name);
CREATE INDEX IF NOT EXISTS idx_nodes_file_path ON nodes(file_path);

CREATE TABLE IF NOT EXISTS edges (
    id INTEGER PRIMARY KEY,
    source_node_id INTEGER NOT NULL,
    target_node_id INTEGER NOT NULL,
    edge_type TEXT NOT NULL,
    line INTEGER NOT NULL DEFAULT 0,
    FOREIGN KEY (source_node_id) REFERENCES nodes(id),
    FOREIGN KEY (target_node_id) REFERENCES nodes(id)
);
CREATE INDEX IF NOT EXISTS idx_edges_source ON edges(source_node_id);
CREATE INDEX IF NOT EXISTS idx_edges_target ON edges(target_node_id);

CREATE TABLE IF NOT EXISTS files (
    path TEXT PRIMARY KEY,
    mtype TEXT NOT NULL DEFAULT '',
    symbol_count INTEGER NOT NULL DEFAULT 0,
    last_indexed TEXT NOT NULL
);
`
