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
// them. This is the first-time index.
func (s *SQLiteStore) IndexAll(ctx context.Context, parseFile FileParser) error {
	indexed := make(map[string]bool)

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

		// Read and index file
		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		if err := s.indexFileByPath(ctx, relPath, content, parseFile); err != nil {
			return err
		}
		indexed[relPath] = true

		return nil
	})
	if err != nil {
		return fmt.Errorf("index all: %w", err)
	}

	// Only clean up orphaned file records on a successful full walk.
	// If the walk failed partway through, removing "missing" files would
	// delete entries for files that were never reached by the walker.
	if err := s.removeDeletedFiles(ctx, indexed); err != nil {
		return fmt.Errorf("remove deleted files: %w", err)
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
func (s *SQLiteStore) IndexChangedFiles(ctx context.Context, parseFile FileParser) error {
	staleFiles, err := s.GetStaleFiles(ctx)
	if err != nil {
		return fmt.Errorf("get stale files: %w", err)
	}

	for _, path := range staleFiles {
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
		if os.IsNotExist(err) {
			// File was deleted — remove from index.
			if err := s.deleteFileFromIndex(ctx, path); err != nil {
				return fmt.Errorf("delete file %s from index: %w", path, err)
			}
			continue
		}
		if err != nil {
			continue // skip files we can't read
		}

		if err := s.indexFileByPath(ctx, path, content, parseFile); err != nil {
			return fmt.Errorf("index file %s: %w", path, err)
		}
	}

	return nil
}

// indexFileByPath is a helper that parses a single file and indexes it.
func (s *SQLiteStore) indexFileByPath(ctx context.Context, path string, content []byte, parseFile FileParser) error {
	symbols, edges, err := parseFile(path, content)
	if err != nil {
		return fmt.Errorf("parse file %s: %w", path, err)
	}

	// Get file mtime for the FileMTime field.
	absPath := path
	if !filepath.IsAbs(path) {
		absPath = filepath.Join(s.baseDir, path)
	}
	var mtime string
	if info, err := os.Stat(absPath); err == nil {
		mtime = info.ModTime().UTC().Format(time.RFC3339)
	}

	// Set FileMTime on all symbols.
	for i := range symbols {
		symbols[i].FileMTime = mtime
		symbols[i].FilePath = path
	}

	return s.IndexFile(ctx, path, symbols, edges)
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
