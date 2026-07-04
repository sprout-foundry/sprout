//go:build !js

package codegraph

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"

	"github.com/sprout-foundry/sprout/pkg/filesystem"
	"github.com/sprout-foundry/sprout/pkg/git"
)

// Symbol represents a code symbol (function, type, variable, etc.)
type Symbol struct {
	ID              int64  // database node ID (populated by queries)
	QualifiedName   string // e.g. "pkg/codegraph.Store.IndexFile"
	DisplayName     string // e.g. "IndexFile"
	FilePath        string // relative path from git root
	Line            int    // line where symbol is declared
	Kind            string // "func", "type", "var", "const", "iface", "method"
	Language        string // "go", "typescript", "javascript", "python"
	FileMTime       string // file modification time as RFC3339 string
}

// Edge represents a relationship between two symbols.
// SourceQualifiedName and TargetQualifiedName are the qualified names
// of the caller and callee respectively. They are resolved to node IDs
// during IndexFile.
// EdgeType values:
//   "calls"          - textual/unresolved call edge (same-package or unresolved cross-package)
//   "resolved_calls" - resolved cross-package call edge (target qualified via import map)
//   "defined_in"     - symbol defined in a file/package
//   "imports"        - module-level import relationship
type Edge struct {
	SourceQualifiedName string // qualified name of the caller/owner
	TargetQualifiedName string // qualified name of the callee/target
	EdgeType            string // "calls", "defined_in", "imports"
	Line                int    // line where the edge originates
}

// GraphStats provides summary statistics about the graph
type GraphStats struct {
	NodeCount int
	EdgeCount int
	FileCount int
}

// Store defines the persistent graph store interface
type Store interface {
	// IndexFile stores all symbols and edges for a given file.
	// It replaces existing data for this file path (delete old nodes/edges, insert new).
	IndexFile(ctx context.Context, path string, symbols []Symbol, edges []Edge) error

	// InsertAllEdges inserts all call edges in a single transaction.
	// All nodes must already exist in the database (call IndexFile or
	// indexSymbolsOnly first). Deletes ALL existing edges first, then
	// inserts the new set — this is correct for a full rebuild via IndexAll.
	InsertAllEdges(ctx context.Context, edges []Edge) error

	// QueryCallers returns symbols that call the given qualified name.
	QueryCallers(ctx context.Context, qualifiedName string) ([]Symbol, error)

	// QueryCallees returns symbols that are called by the given qualified name.
	QueryCallees(ctx context.Context, qualifiedName string) ([]Symbol, error)

	// FindDeadCode returns symbols with zero inbound call edges.
	// Excludes known entry points: main(), init(), exported functions, and test functions.
	// If directory is non-empty, restricts results to files under that directory prefix.
	FindDeadCode(ctx context.Context, directory string) ([]Symbol, error)

	// GetStaleFiles returns file paths whose mtime differs from the last indexed time.
	GetStaleFiles(ctx context.Context) ([]string, error)

	// Stats returns summary statistics about the graph.
	Stats() GraphStats

	// QueryAllNodes returns all nodes from the graph store.
	QueryAllNodes(ctx context.Context) ([]Symbol, error)

	// Close closes the underlying database.
	Close() error

	// BaseDir returns the git root directory for resolving relative file paths.
	BaseDir() string
}

// SQLiteStore implements Store using a SQLite database.
type SQLiteStore struct {
	db      *sql.DB
	dbPath  string
	baseDir string // git root directory for resolving relative file paths
	mu      sync.RWMutex
}

// DefaultDBPath returns the default database path resolved from git root.
func DefaultDBPath() (string, error) {
	gitRoot, err := git.GetGitRootDir()
	if err != nil {
		return "", fmt.Errorf("failed to resolve git root for default DB path: %w", err)
	}
	return filepath.Join(gitRoot, ".sprout", "codegraph.db"), nil
}

// NewStore opens a SQLite-backed code graph store at the given path.
// If dbPath is empty, the database is placed at `.sprout/codegraph.db`
// relative to the git root.
func NewStore(dbPath string) (*SQLiteStore, error) {
	if dbPath == "" {
		var err error
		dbPath, err = DefaultDBPath()
		if err != nil {
			return nil, err
		}
	}

	// Ensure the parent directory exists.
	dir := filepath.Dir(dbPath)
	if err := filesystem.EnsureDir(dir); err != nil {
		return nil, fmt.Errorf("failed to create database directory %s: %w", dir, err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Limit connection pool to 1 for SQLite safety.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	// Enable WAL mode for better concurrent read performance.
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to set WAL mode: %w", err)
	}

	// Enable foreign keys.
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to enable foreign keys: %w", err)
	}

	// Run schema creation.
	if _, err := db.Exec(schemaSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create schema: %w", err)
	}

	// Resolve the git root (base directory) for resolving relative file paths.
	baseDir, err := git.GetGitRootDir()
	if err != nil {
		baseDir = filepath.Dir(dbPath)
	}

	return &SQLiteStore{
		db:      db,
		dbPath:  dbPath,
		baseDir: baseDir,
	}, nil
}

// BaseDir returns the git root directory used for resolving relative file paths.
func (s *SQLiteStore) BaseDir() string {
	return s.baseDir
}

// IndexFile stores all symbols and edges for a given file.
// It replaces existing data for this file path (delete old nodes/edges, insert new).
func (s *SQLiteStore) IndexFile(ctx context.Context, path string, symbols []Symbol, edges []Edge) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	// Delete existing edges referencing nodes from this file.
	_, err = tx.ExecContext(ctx, `
		DELETE FROM edges WHERE source_node_id IN (SELECT id FROM nodes WHERE file_path = ?)
		   OR target_node_id IN (SELECT id FROM nodes WHERE file_path = ?)
	`, path, path)
	if err != nil {
		return fmt.Errorf("failed to delete existing edges: %w", err)
	}

	// Delete existing nodes for this file.
	_, err = tx.ExecContext(ctx, `DELETE FROM nodes WHERE file_path = ?`, path)
	if err != nil {
		return fmt.Errorf("failed to delete existing nodes: %w", err)
	}

	// Insert new symbols and capture their IDs.
	type insertResult struct {
		symbol Symbol
		id     int64
	}
	var results []insertResult

	for _, sym := range symbols {
		var res sql.Result
		res, err = tx.ExecContext(ctx, `
			INSERT INTO nodes (qualified_name, display_name, file_path, line, kind, language, file_mtime)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, sym.QualifiedName, sym.DisplayName, path, sym.Line, sym.Kind, sym.Language, sym.FileMTime)
		if err != nil {
			return fmt.Errorf("failed to insert node %s: %w", sym.QualifiedName, err)
		}
		var id int64
		id, err = res.LastInsertId()
		if err != nil {
			return fmt.Errorf("failed to get last insert ID for %s: %w", sym.QualifiedName, err)
		}
		results = append(results, insertResult{symbol: sym, id: id})
	}

	// Build a map from qualified_name → node_id for the newly inserted nodes.
	qnToID := make(map[string]int64, len(results))
	for _, r := range results {
		qnToID[r.symbol.QualifiedName] = r.id
	}

	// Insert edges using the captured node IDs.
	for _, edge := range edges {
		// Resolve source qualified name to node ID.
		sourceID, ok := qnToID[edge.SourceQualifiedName]
		if !ok {
			// Source not in this batch — try querying the database within the transaction.
			var srcErr error
			srcErr = tx.QueryRowContext(ctx,
				`SELECT id FROM nodes WHERE qualified_name = ?`, edge.SourceQualifiedName).Scan(&sourceID)
			if srcErr != nil {
				// Skip edges where we can't resolve the source.
				continue
			}
		}

		// Resolve target qualified name to node ID.
		targetID, ok := qnToID[edge.TargetQualifiedName]
		if !ok {
			var tgtErr error
			tgtErr = tx.QueryRowContext(ctx,
				`SELECT id FROM nodes WHERE qualified_name = ?`, edge.TargetQualifiedName).Scan(&targetID)
			if tgtErr != nil {
				// Skip edges where we can't resolve the target.
				continue
			}
		}

		_, err = tx.ExecContext(ctx, `
			INSERT INTO edges (source_node_id, target_node_id, edge_type, line)
			VALUES (?, ?, ?, ?)
		`, sourceID, targetID, edge.EdgeType, edge.Line)
		if err != nil {
			return fmt.Errorf("failed to insert edge: %w", err)
		}
	}

	// Determine language from symbols.
	lang := ""
	if len(symbols) > 0 {
		lang = symbols[0].Language
	}

	// Upsert into files table.
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = tx.ExecContext(ctx, `
		INSERT OR REPLACE INTO files (path, mtype, symbol_count, last_indexed)
		VALUES (?, ?, ?, ?)
	`, path, lang, len(symbols), now)
	if err != nil {
		return fmt.Errorf("failed to upsert file record: %w", err)
	}

	err = tx.Commit()
	if err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// InsertAllEdges inserts all call edges in a single transaction.
// All nodes must already exist in the database (call IndexFile or
// indexSymbolsOnly first). Deletes ALL existing edges, then inserts
// the new set. This is correct for a full rebuild via IndexAll.
func (s *SQLiteStore) InsertAllEdges(ctx context.Context, edges []Edge) error {
	if len(edges) == 0 {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	// Delete ALL existing edges (clean slate for full rebuild).
	_, err = tx.ExecContext(ctx, `DELETE FROM edges`)
	if err != nil {
		return fmt.Errorf("delete all edges: %w", err)
	}

	// Insert edges by resolving qualified names from the database.
	// All nodes must already exist in the DB for cross-file resolution to work.
	for _, edge := range edges {
		var sourceID, targetID int64

		srcErr := tx.QueryRowContext(ctx,
			`SELECT id FROM nodes WHERE qualified_name = ?`, edge.SourceQualifiedName).Scan(&sourceID)
		if srcErr != nil {
			// Skip edges where we can't resolve the source.
			continue
		}

		tgtErr := tx.QueryRowContext(ctx,
			`SELECT id FROM nodes WHERE qualified_name = ?`, edge.TargetQualifiedName).Scan(&targetID)
		if tgtErr != nil {
			// Skip edges where we can't resolve the target.
			continue
		}

		_, err = tx.ExecContext(ctx, `
			INSERT INTO edges (source_node_id, target_node_id, edge_type, line)
			VALUES (?, ?, ?, ?)
		`, sourceID, targetID, edge.EdgeType, edge.Line)
		if err != nil {
			return fmt.Errorf("insert edge: %w", err)
		}
	}

	return tx.Commit()
}

// QueryCallers returns symbols that call the given qualified name.
func (s *SQLiteStore) QueryCallers(ctx context.Context, qualifiedName string) ([]Symbol, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT n.id, n.qualified_name, n.display_name, n.file_path, n.line, n.kind, n.language, n.file_mtime
		FROM nodes n
		JOIN edges e ON e.source_node_id = n.id
		JOIN nodes target ON e.target_node_id = target.id
		WHERE target.qualified_name = ?
		AND (e.edge_type = 'resolved_calls' OR e.edge_type = 'calls')
		ORDER BY n.qualified_name
	`, qualifiedName)
	if err != nil {
		return nil, fmt.Errorf("failed to query callers: %w", err)
	}
	defer rows.Close()

	var symbols []Symbol
	for rows.Next() {
		var sym Symbol
		if err := rows.Scan(&sym.ID, &sym.QualifiedName, &sym.DisplayName, &sym.FilePath,
			&sym.Line, &sym.Kind, &sym.Language, &sym.FileMTime); err != nil {
			return nil, fmt.Errorf("failed to scan caller row: %w", err)
		}
		symbols = append(symbols, sym)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration error: %w", err)
	}

	return symbols, nil
}

// QueryCallees returns symbols that are called by the given qualified name.
func (s *SQLiteStore) QueryCallees(ctx context.Context, qualifiedName string) ([]Symbol, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT n.id, n.qualified_name, n.display_name, n.file_path, n.line, n.kind, n.language, n.file_mtime
		FROM nodes n
		JOIN edges e ON e.target_node_id = n.id
		JOIN nodes source ON e.source_node_id = source.id
		WHERE source.qualified_name = ?
		AND (e.edge_type = 'resolved_calls' OR e.edge_type = 'calls')
		ORDER BY n.qualified_name
	`, qualifiedName)
	if err != nil {
		return nil, fmt.Errorf("failed to query callees: %w", err)
	}
	defer rows.Close()

	var symbols []Symbol
	for rows.Next() {
		var sym Symbol
		if err := rows.Scan(&sym.ID, &sym.QualifiedName, &sym.DisplayName, &sym.FilePath,
			&sym.Line, &sym.Kind, &sym.Language, &sym.FileMTime); err != nil {
			return nil, fmt.Errorf("failed to scan callee row: %w", err)
		}
		symbols = append(symbols, sym)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration error: %w", err)
	}

	return symbols, nil
}

// FindDeadCode returns symbols with zero inbound call edges.
// Excludes known entry points: main(), init(), exported functions, and test functions.
// If directory is non-empty, restricts results to files under that directory prefix.
func (s *SQLiteStore) FindDeadCode(ctx context.Context, directory string) ([]Symbol, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `
		SELECT n.id, n.qualified_name, n.display_name, n.file_path, n.line, n.kind, n.language, n.file_mtime
		FROM nodes n
		WHERE n.id NOT IN (
			SELECT DISTINCT e.target_node_id FROM edges e WHERE e.edge_type IN ('resolved_calls', 'calls')
		)
		AND n.kind NOT IN ('type', 'var', 'const', 'iface')
	`
	var args []interface{}
	if directory != "" {
		query += ` AND n.file_path LIKE ?`
		args = append(args, directory+"/%")
	}
	query += ` ORDER BY n.qualified_name`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query dead code: %w", err)
	}
	defer rows.Close()

	var symbols []Symbol
	for rows.Next() {
		var sym Symbol
		if err := rows.Scan(&sym.ID, &sym.QualifiedName, &sym.DisplayName, &sym.FilePath,
			&sym.Line, &sym.Kind, &sym.Language, &sym.FileMTime); err != nil {
			return nil, fmt.Errorf("failed to scan dead code row: %w", err)
		}
		// Skip methods (Go methods have parenthesized receiver syntax like
		// "pkg.(*Type).Method" or "pkg.Type.Method" with more than one
		// path-component-qualified segment after the last "/").
		lastSlash := strings.LastIndex(sym.QualifiedName, "/")
		afterSlash := sym.QualifiedName
		if lastSlash >= 0 {
			afterSlash = sym.QualifiedName[lastSlash+1:]
		}
		// Methods have: (a) parenthesized receiver, or (b) >1 dot after last "/"
		// (package-level functions have exactly 1 dot: "pkgname.FuncName")
		if strings.Contains(afterSlash, "(") || strings.Count(afterSlash, ".") > 1 {
			continue
		}
		// Skip exported functions (first char is uppercase in Go).
		if len(sym.DisplayName) > 0 && sym.DisplayName[0] >= 'A' && sym.DisplayName[0] <= 'Z' {
			continue
		}
		// Skip test functions.
		if strings.HasPrefix(sym.DisplayName, "Test") {
			continue
		}
		// Skip init() and main().
		if sym.DisplayName == "init" || sym.DisplayName == "main" {
			continue
		}
		symbols = append(symbols, sym)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration error: %w", err)
	}

	return symbols, nil
}

// GetStaleFiles returns file paths whose on-disk mtime is newer than the
// stored last_indexed timestamp. Deleted files are also reported as stale.
func (s *SQLiteStore) GetStaleFiles(ctx context.Context) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.QueryContext(ctx, `SELECT path, last_indexed FROM files`)
	if err != nil {
		return nil, fmt.Errorf("failed to query files: %w", err)
	}
	defer rows.Close()

	var staleFiles []string
	for rows.Next() {
		var path, lastIndexed string
		if err := rows.Scan(&path, &lastIndexed); err != nil {
			return nil, fmt.Errorf("failed to scan file row: %w", err)
		}

		// Check if the file still exists on disk.
		// Resolve relative paths against the git root; leave absolute paths as-is.
		absPath := path
		if !filepath.IsAbs(path) {
			absPath = filepath.Join(s.baseDir, path)
		}
		info, err := os.Stat(absPath)
		if os.IsNotExist(err) {
			// File was deleted — consider it stale.
			staleFiles = append(staleFiles, path)
			continue
		}
		if err != nil {
			// Can't stat — skip silently.
			continue
		}

		// Compare disk mtime with stored last_indexed.
		diskTime := info.ModTime().UTC()
		indexedTime, err := time.Parse(time.RFC3339, lastIndexed)
		if err != nil {
			// Can't parse stored time — treat as stale to be safe.
			staleFiles = append(staleFiles, path)
			continue
		}
		if diskTime.After(indexedTime) {
			staleFiles = append(staleFiles, path)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration error: %w", err)
	}

	return staleFiles, nil
}

// QueryAllNodes returns all nodes from the graph store.
func (s *SQLiteStore) QueryAllNodes(ctx context.Context) ([]Symbol, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, qualified_name, display_name, file_path, line, kind, language, file_mtime
		FROM nodes ORDER BY file_path, line
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query all nodes: %w", err)
	}
	defer rows.Close()

	var symbols []Symbol
	for rows.Next() {
		var sym Symbol
		if err := rows.Scan(&sym.ID, &sym.QualifiedName, &sym.DisplayName, &sym.FilePath,
			&sym.Line, &sym.Kind, &sym.Language, &sym.FileMTime); err != nil {
			return nil, fmt.Errorf("failed to scan node row: %w", err)
		}
		symbols = append(symbols, sym)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration error: %w", err)
	}

	return symbols, nil
}

// Stats returns summary statistics about the graph.
func (s *SQLiteStore) Stats() GraphStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var stats GraphStats

	var err error
	stats.NodeCount, err = s.queryCount("nodes")
	if err != nil {
		stats.NodeCount = 0
	}

	stats.EdgeCount, err = s.queryCount("edges")
	if err != nil {
		stats.EdgeCount = 0
	}

	stats.FileCount, err = s.queryCount("files")
	if err != nil {
		stats.FileCount = 0
	}

	return stats
}

func (s *SQLiteStore) queryCount(table string) (int, error) {
	var query string
	switch table {
	case "nodes", "edges", "files":
		query = "SELECT COUNT(*) FROM " + table
	default:
		return 0, fmt.Errorf("unknown table: %s", table)
	}
	var count int
	err := s.db.QueryRow(query).Scan(&count)
	return count, err
}

// Close closes the underlying database.
func (s *SQLiteStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}
