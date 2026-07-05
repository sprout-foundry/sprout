//go:build !js

package codegraph

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// InsertAllEdges inserts all call edges in a single transaction.
// All nodes must already exist in the database (call IndexFile or
// indexSymbolsOnly first). Deletes ALL existing edges first, then
// inserts the new set — this is correct for a full rebuild via IndexAll.
func (s *SQLiteStore) InsertAllEdges(ctx context.Context, edges []Edge) error {
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
	// This runs even when edges is empty to honor the contract that
	// InsertAllEdges wipes the edge table — e.g. when a repo's parse
	// produces zero edges but stale edges exist from a prior build.
	_, err = tx.ExecContext(ctx, `DELETE FROM edges`)
	if err != nil {
		return fmt.Errorf("delete all edges: %w", err)
	}

	if len(edges) == 0 {
		return tx.Commit()
	}

	// Insert edges by resolving qualified names from the database.
	// All nodes must already exist in the DB for cross-file resolution to work.
	for _, edge := range edges {
		sourceID, srcFound := resolveEdgeNode(ctx, tx, edge.SourceQualifiedName)
		if !srcFound {
			continue
		}

		targetID, tgtFound := resolveEdgeNode(ctx, tx, edge.TargetQualifiedName)
		if !tgtFound {
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

// resolveEdgeNode resolves a qualified name to a node ID, with fallbacks for
// cross-file and unresolved call edges. Resolution order:
//  1. Exact qualified_name match.
//  2. Suffix-match on qualified_name (e.g. "DoWork" → "pkg/lib.DoWork"),
//     only when exactly one match exists.
//  3. display_name match (e.g. bare "ProcessQuery" → the node whose
//     display_name is "ProcessQuery"), only when exactly one match exists.
//
// Method calls like `ag.ProcessQuery()` are stripped to the bare method name
// "ProcessQuery" by the extractor (the receiver variable type is unknown
// without full type inference). The display_name fallback (step 3) resolves
// these when unambiguous — i.e. only one node defines a method/function with
// that display name. When multiple nodes share the display name, the edge is
// dropped rather than guessing.
func resolveEdgeNode(ctx context.Context, tx *sql.Tx, qualifiedName string) (int64, bool) {
	// Step 1: exact qualified_name match.
	var id int64
	err := tx.QueryRowContext(ctx,
		`SELECT id FROM nodes WHERE qualified_name = ?`, qualifiedName).Scan(&id)
	if err == nil {
		return id, true
	}

	// Strip a leading receiver variable prefix (e.g. "ag.ProcessQuery" →
	// "ProcessQuery") so the suffix/display-name fallbacks can fire. The
	// extractor strips import-qualified prefixes but leaves receiver
	// variables like "ag." or "s." intact.
	leafName := qualifiedName
	if dotIdx := strings.LastIndexByte(qualifiedName, '.'); dotIdx >= 0 && !strings.Contains(qualifiedName, "(") {
		leafName = qualifiedName[dotIdx+1:]
	}

	// Step 2: suffix-match on qualified_name, anchored on "." so "DoWork"
	// matches "pkg/lib.DoWork" but not "pkg/lib.SuperDoWork". Escape LIKE
	// wildcards in the leaf name to prevent "_" and "%" from acting as
	// pattern matchers (e.g. a function named "process_query").
	escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(leafName)
	rows, qErr := tx.QueryContext(ctx,
		`SELECT id FROM nodes WHERE qualified_name LIKE '%.' || ? ESCAPE '\' AND qualified_name != ?`,
		escaped, qualifiedName)
	if qErr == nil {
		var ids []int64
		for rows.Next() {
			var nid int64
			if scanErr := rows.Scan(&nid); scanErr == nil {
				ids = append(ids, nid)
			}
		}
		rows.Close()
		if len(ids) == 1 {
			return ids[0], true
		}
	}

	// Step 3: display_name match. Handles bare method names ("ProcessQuery")
	// whose qualified_name differs ("pkg/agent.(*Agent).ProcessQuery").
	// Only resolves when exactly one node has this display_name to avoid
	// linking to the wrong overload. Note: common names like "Close" or "Init"
	// typically have multiple matches across a codebase and are correctly
	// skipped; in small/partially-indexed repos this may resolve calls to
	// external dependencies (e.g. io.Closer) to the single internal match.
	rows2, qErr2 := tx.QueryContext(ctx,
		`SELECT id FROM nodes WHERE display_name = ?`, leafName)
	if qErr2 == nil {
		var ids []int64
		for rows2.Next() {
			var nid int64
			if scanErr := rows2.Scan(&nid); scanErr == nil {
				ids = append(ids, nid)
			}
		}
		rows2.Close()
		if len(ids) == 1 {
			return ids[0], true
		}
	}
	return 0, false
}

// InsertEdgesForFiles deletes and re-inserts edges for a scoped set of files.
// It is the scoped equivalent of InsertAllEdges used by the incremental update
// path: rather than wiping the entire edge table, it only touches edges from
// the affected files.
//
// stalePaths: files whose symbols were just re-indexed (nodes deleted + recreated).
//   All edges touching these files (incoming AND outgoing) are deleted.
// referrerPaths: files whose symbols are unchanged but whose outgoing edges
//   may reference changed/removed nodes in stale files. Only their OUTGOING
//   edges are deleted and re-resolved; incoming edges are preserved.
//
// All nodes referenced by the edges must already exist in the database.
func (s *SQLiteStore) InsertEdgesForFiles(ctx context.Context, stalePaths, referrerPaths []string, edges []Edge) error {
	if len(edges) == 0 && len(stalePaths) == 0 && len(referrerPaths) == 0 {
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

	// Stale files: nodes were deleted and recreated. Delete ALL edges touching
	// these files (incoming + outgoing) — they all need re-resolution.
	for _, fp := range stalePaths {
		_, err = tx.ExecContext(ctx, `
			DELETE FROM edges WHERE source_node_id IN (SELECT id FROM nodes WHERE file_path = ?)
			   OR target_node_id IN (SELECT id FROM nodes WHERE file_path = ?)
		`, fp, fp)
		if err != nil {
			return fmt.Errorf("delete edges for stale file %s: %w", fp, err)
		}
	}

	// Referrer files: symbols unchanged. Only their OUTGOING edges need
	// re-resolution (targets may have changed). Preserve incoming edges —
	// the referrer's own nodes still exist and edges pointing to them are valid.
	for _, fp := range referrerPaths {
		_, err = tx.ExecContext(ctx, `
			DELETE FROM edges WHERE source_node_id IN (SELECT id FROM nodes WHERE file_path = ?)
		`, fp)
		if err != nil {
			return fmt.Errorf("delete outgoing edges for referrer %s: %w", fp, err)
		}
	}

	// Re-insert edges from the freshly-parsed affected files.
	for _, edge := range edges {
		sourceID, srcFound := resolveEdgeNode(ctx, tx, edge.SourceQualifiedName)
		if !srcFound {
			continue
		}
		targetID, tgtFound := resolveEdgeNode(ctx, tx, edge.TargetQualifiedName)
		if !tgtFound {
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

// FindReferrerFiles returns the set of file paths whose nodes have edges
// pointing to nodes in any of the given file paths. This identifies files
// whose outgoing edges may become stale when a callee file is re-indexed.
// Used by the incremental update path to compute the affected-file closure.
func (s *SQLiteStore) FindReferrerFiles(ctx context.Context, filePaths []string) ([]string, error) {
	if len(filePaths) == 0 {
		return nil, nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	// Build a parameterized IN-list placeholder string: ?,?,...
	// filePaths is bounded by the number of stale files (small in practice).
	placeholders := make([]string, len(filePaths))
	args := make([]interface{}, len(filePaths))
	for i, fp := range filePaths {
		placeholders[i] = "?"
		args[i] = fp
	}
	placeholderStr := strings.Join(placeholders, ",")

	query := `
		SELECT DISTINCT n_source.file_path
		FROM edges e
		JOIN nodes n_source ON e.source_node_id = n_source.id
		JOIN nodes n_target ON e.target_node_id = n_target.id
		WHERE n_target.file_path IN (` + placeholderStr + `)
	`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query referrer files: %w", err)
	}
	defer rows.Close()

	var referrers []string
	seen := make(map[string]bool)
	for rows.Next() {
		var fp string
		if err := rows.Scan(&fp); err != nil {
			return nil, fmt.Errorf("scan referrer file: %w", err)
		}
		if !seen[fp] {
			seen[fp] = true
			referrers = append(referrers, fp)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration error: %w", err)
	}
	return referrers, nil
}
