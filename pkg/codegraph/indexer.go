//go:build !js

package codegraph

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// FileParser is a function that parses a source file at the given path and
// returns the symbols and edges to index. Implementations can call external
// parsers (e.g., from pkg/agent_tools) to extract this information.
type FileParser func(path string, content []byte) ([]Symbol, []Edge, error)

// sourceExtensions is the set of file extensions to index.
// This is intentionally a subset of repo_map.go's sourceExtensions
// because ExtractCallsAndSymbols only supports these languages.
// Rust (.rs), Java (.java), and C/C++ (.c, .cpp, .h) files are visible
// in repo_map output but cannot be parsed for call-graph indexing.
var sourceExtensions = map[string]bool{
	".go": true, ".ts": true, ".tsx": true, ".js": true, ".jsx": true,
	".py": true,
}

// ignoredDirs are directory names to skip during file walking.
var ignoredDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, "dist": true,
	"build": true, ".next": true, "coverage": true, ".cache": true, ".sprout": true,
}

// IndexAll performs a full walk of all source files in the repo and indexes
// them. Uses two-phase indexing: first inserts all symbols, then inserts all
// edges. This ensures cross-file call edges resolve correctly since all nodes
// exist in the database before edges are inserted.
func (s *SQLiteStore) IndexAll(ctx context.Context, parseFile FileParser) error {
	indexed := make(map[string]bool)
	// Accumulate edges from all files for phase 2 (cross-file edge resolution).
	var allEdges []Edge

	err := filepath.WalkDir(s.baseDir, func(path string, d os.DirEntry, err error) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if err != nil {
			return nil
		}
		name := d.Name()

		// Skip symlinks
		if d.Type()&os.ModeSymlink != 0 {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip ignored/hidden directories
		if d.IsDir() {
			if ignoredDirs[name] {
				return filepath.SkipDir
			}
			if path != s.baseDir && strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return nil
		}

		// Check extension
		ext := strings.ToLower(filepath.Ext(name))
		if !sourceExtensions[ext] {
			return nil
		}

		// Skip _test.go files
		if ext == ".go" && strings.HasSuffix(name, "_test.go") {
			return nil
		}

		// Compute relative path from baseDir
		rel, err := filepath.Rel(s.baseDir, path)
		if err != nil {
			return nil
		}
		relPath := filepath.ToSlash(rel)

		// Read and parse file
		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		symbols, edges, err := parseFile(relPath, content)
		if err != nil {
			return fmt.Errorf("parse file %s: %w", relPath, err)
		}

		// Get file mtime for the FileMTime field.
		var mtime string
		if info, err := os.Stat(path); err == nil {
			mtime = info.ModTime().UTC().Format(time.RFC3339)
		}

		// Set FileMTime and FilePath on all symbols.
		for i := range symbols {
			symbols[i].FileMTime = mtime
			symbols[i].FilePath = relPath
		}

		// Phase 1: insert symbols only (no edges).
		if err := s.indexSymbolsOnly(ctx, relPath, symbols); err != nil {
			return err
		}

		// Accumulate edges for phase 2.
		allEdges = append(allEdges, edges...)
		indexed[relPath] = true

		return nil
	})
	if err != nil {
		return fmt.Errorf("index all: %w", err)
	}

	// Phase 2: insert all edges in a single transaction, after all nodes
	// exist in the DB. This enables cross-file qualified-name resolution.
	if err := s.InsertAllEdges(ctx, allEdges); err != nil {
		return fmt.Errorf("insert all edges: %w", err)
	}

	// Only clean up orphaned file records on a successful full walk.
	// If the walk failed partway through, removing "missing" files would
	// delete entries for files that were never reached by the walker.
	if err := s.removeDeletedFiles(ctx, indexed); err != nil {
		return fmt.Errorf("remove deleted files: %w", err)
	}

	return nil
}

// indexSymbolsOnly inserts only symbols (nodes) and the file record for a file,
// without inserting any edges. This is used by IndexAll's first phase to ensure
// all nodes exist in the database before edges are inserted in the second phase,
// enabling cross-file edge resolution.
func (s *SQLiteStore) indexSymbolsOnly(ctx context.Context, path string, symbols []Symbol) error {
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

	// Delete existing edges referencing nodes from this file.
	_, err = tx.ExecContext(ctx, `
		DELETE FROM edges WHERE source_node_id IN (SELECT id FROM nodes WHERE file_path = ?)
		   OR target_node_id IN (SELECT id FROM nodes WHERE file_path = ?)
	`, path, path)
	if err != nil {
		return fmt.Errorf("delete edges for %s: %w", path, err)
	}

	// Delete existing nodes for this file.
	_, err = tx.ExecContext(ctx, `DELETE FROM nodes WHERE file_path = ?`, path)
	if err != nil {
		return fmt.Errorf("delete nodes for %s: %w", path, err)
	}

	// Insert new symbols.
	for _, sym := range symbols {
		_, err = tx.ExecContext(ctx, `
			INSERT OR IGNORE INTO nodes (qualified_name, display_name, file_path, line, kind, language, file_mtime)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, sym.QualifiedName, sym.DisplayName, path, sym.Line, sym.Kind, sym.Language, sym.FileMTime)
		if err != nil {
			return fmt.Errorf("insert node %s: %w", sym.QualifiedName, err)
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
		return fmt.Errorf("upsert file record: %w", err)
	}

	err = tx.Commit()
	if err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	return nil
}

// removeDeletedFiles removes entries from the files table for files that
// are no longer present in the indexed set.
func (s *SQLiteStore) removeDeletedFiles(ctx context.Context, indexed map[string]bool) error {
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

	// Query all file paths from the DB.
	rows, err := tx.QueryContext(ctx, `SELECT path FROM files`)
	if err != nil {
		return fmt.Errorf("query files: %w", err)
	}

	var pathsToDelete []string
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			rows.Close()
			return fmt.Errorf("scan file path: %w", err)
		}
		if !indexed[path] {
			pathsToDelete = append(pathsToDelete, path)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("row iteration error: %w", err)
	}

	// Delete stale files and their associated nodes/edges.
	for _, path := range pathsToDelete {
		if err := s.deleteFileRecords(tx, ctx, path); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// IndexChangedFiles indexes only files whose on-disk mtime differs from
// last_indexed. Uses GetStaleFiles internally.
//
// Uses a scoped two-phase approach mirroring IndexAll to preserve cross-file
// edges correctly: (1) compute the closure of affected files (stale files +
// files whose edges point into them) BEFORE any deletion, (2) re-index symbols
// for stale files, (3) re-parse and bulk-insert edges for the entire closure.
//
// Referrer files (those with edges into stale files) only have their OUTGOING
// edges deleted and re-resolved; their incoming edges are preserved. This
// prevents progressive edge loss in multi-level call chains (X→A→B where only
// B changes must not lose X→A).
func (s *SQLiteStore) IndexChangedFiles(ctx context.Context, parseFile FileParser) error {
	staleFiles, err := s.GetStaleFiles(ctx)
	if err != nil {
		return fmt.Errorf("get stale files: %w", err)
	}
	if len(staleFiles) == 0 {
		return nil
	}

	// Phase 0: Partition stale files into deletions vs re-indexes.
	var toReindex []string
	var deletedFiles []string
	for _, path := range staleFiles {
		absPath := path
		if !filepath.IsAbs(path) {
			absPath = filepath.Join(s.baseDir, path)
		}
		if _, err := os.Stat(absPath); os.IsNotExist(err) {
			deletedFiles = append(deletedFiles, path)
		} else {
			toReindex = append(toReindex, path)
		}
	}

	// Compute referrer closure BEFORE any mutation. Files whose edges point
	// INTO stale files must have their outgoing edges re-resolved against
	// the new symbols. This must happen before indexSymbolsOnly deletes anything.
	referrers, err := s.FindReferrerFiles(ctx, toReindex)
	if err != nil {
		return fmt.Errorf("find referrer files: %w", err)
	}
	referrerSet := make(map[string]bool, len(referrers))
	for _, fp := range referrers {
		referrerSet[fp] = true
	}

	// Handle deletions.
	for _, path := range deletedFiles {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if err := s.deleteFileFromIndex(ctx, path); err != nil {
			return fmt.Errorf("delete file %s from index: %w", path, err)
		}
	}

	// Phase 1: re-index symbols for stale files. Cache parse results to avoid
	// double-parsing in Phase 2 (stale files parsed once here, referrers parsed
	// once in Phase 2).
	type parseCache struct {
		symbols []Symbol
		edges   []Edge
	}
	staleCache := make(map[string]parseCache, len(toReindex))
	for _, path := range toReindex {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		absPath := path
		if !filepath.IsAbs(path) {
			absPath = filepath.Join(s.baseDir, path)
		}
		content, err := os.ReadFile(absPath)
		if err != nil {
			continue
		}
		syms, edges, err := parseAndEnrich(ctx, s.baseDir, path, content, parseFile)
		if err != nil {
			return fmt.Errorf("parse file %s: %w", path, err)
		}
		staleCache[path] = parseCache{syms, edges}
		if err := s.indexSymbolsOnly(ctx, path, syms); err != nil {
			return fmt.Errorf("index symbols for %s: %w", path, err)
		}
	}

	if len(toReindex) == 0 && len(referrerSet) == 0 {
		return nil
	}

	// Phase 2: collect edges from stale files (cached) + referrer files (parsed).
	var allEdges []Edge
	for _, path := range toReindex {
		allEdges = append(allEdges, staleCache[path].edges...)
	}
	for fp := range referrerSet {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		absPath := fp
		if !filepath.IsAbs(fp) {
			absPath = filepath.Join(s.baseDir, fp)
		}
		content, err := os.ReadFile(absPath)
		if err != nil {
			continue
		}
		_, edges, err := parseAndEnrich(ctx, s.baseDir, fp, content, parseFile)
		if err != nil {
			continue
		}
		allEdges = append(allEdges, edges...)
	}

	referrerList := make([]string, 0, len(referrerSet))
	for fp := range referrerSet {
		referrerList = append(referrerList, fp)
	}
	if err := s.InsertEdgesForFiles(ctx, toReindex, referrerList, allEdges); err != nil {
		return fmt.Errorf("reinsert edges for affected files: %w", err)
	}

	return nil
}

// parseAndEnrich parses a file and enriches symbols with file path and mtime.
func parseAndEnrich(ctx context.Context, baseDir, path string, content []byte, parseFile FileParser) ([]Symbol, []Edge, error) {
	symbols, edges, err := parseFile(path, content)
	if err != nil {
		return nil, nil, err
	}
	absPath := path
	if !filepath.IsAbs(path) {
		absPath = filepath.Join(baseDir, path)
	}
	var mtime string
	if info, err := os.Stat(absPath); err == nil {
		mtime = info.ModTime().UTC().Format(time.RFC3339)
	}
	for i := range symbols {
		symbols[i].FileMTime = mtime
		symbols[i].FilePath = path
	}
	return symbols, edges, nil
}

// deleteFileRecords removes all traces of a file from the graph database
// within an existing transaction. It deletes edges referencing the file's
// nodes, the nodes themselves, and the file record.
func (s *SQLiteStore) deleteFileRecords(tx *sql.Tx, ctx context.Context, path string) error {
	// Delete edges referencing nodes from this file.
	_, err := tx.ExecContext(ctx, `
		DELETE FROM edges WHERE source_node_id IN (SELECT id FROM nodes WHERE file_path = ?)
		   OR target_node_id IN (SELECT id FROM nodes WHERE file_path = ?)
	`, path, path)
	if err != nil {
		return fmt.Errorf("delete edges for %s: %w", path, err)
	}

	// Delete nodes for this file.
	_, err = tx.ExecContext(ctx, `DELETE FROM nodes WHERE file_path = ?`, path)
	if err != nil {
		return fmt.Errorf("delete nodes for %s: %w", path, err)
	}

	// Delete from files table.
	_, err = tx.ExecContext(ctx, `DELETE FROM files WHERE path = ?`, path)
	if err != nil {
		return fmt.Errorf("delete file record %s: %w", path, err)
	}

	return nil
}

// deleteFileFromIndex removes all nodes, edges, and the file record for a given path.
func (s *SQLiteStore) deleteFileFromIndex(ctx context.Context, path string) error {
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

	if err = s.deleteFileRecords(tx, ctx, path); err != nil {
		return err
	}

	return tx.Commit()
}
