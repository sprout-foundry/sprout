//go:build !js

package codegraph

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ConfidenceLevel indicates how likely a dead code candidate is to be genuinely dead.
type ConfidenceLevel string

const (
	ConfidenceHigh   ConfidenceLevel = "high"   // Very likely dead: unexported, no registration patterns
	ConfidenceMedium ConfidenceLevel = "medium" // Possibly dead: name or path has minor false-positive hints
	ConfidenceLow    ConfidenceLevel = "low"    // Probably alive: in handler/registration files, name matches known patterns
)

// DeadCodeCandidate is a symbol with zero inbound call edges, plus metadata
// about confidence and whether it has test-only callers.
type DeadCodeCandidate struct {
	Symbol      Symbol
	Confidence  ConfidenceLevel
	TestCallers int // number of callers found in test files (0 = no test callers)
}

// isRegistrationFile returns true if the file path suggests the file registers
// handlers, hooks, or commands via closure/map dispatch.
func isRegistrationFile(path string) bool {
	lower := strings.ToLower(path)
	indicators := []string{
		"handler", "registration", "register", "command", "hook",
		"callback", "middleware", "tool_handlers", "tool_regist",
	}
	for _, ind := range indicators {
		if strings.Contains(lower, ind) {
			return true
		}
	}
	return false
}

// isRegistrationName returns true if the function name suggests it's wired via
// closure, map, or function pointer rather than a direct call.
func isRegistrationName(name string) bool {
	suffixes := []string{
		"Handler", "Hook", "Func", "Fn", "Middleware",
		"Callback", "Factory", "Provider", "Adapter",
	}
	for _, s := range suffixes {
		if strings.HasSuffix(name, s) {
			return true
		}
	}
	prefixes := []string{
		"handle", "Handle", "apply", "Apply", "register", "Register",
	}
	for _, p := range prefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// classifyConfidence assigns a confidence tier to a dead code candidate based
// on heuristics about file paths and function names.
func classifyConfidence(sym Symbol) ConfidenceLevel {
	// TypeScript/JS: almost always false positives (JSX, closures, dynamic dispatch).
	if sym.Language == "typescript" || sym.Language == "javascript" {
		return ConfidenceLow
	}

	// Python: dynamic dispatch is common, default to medium.
	if sym.Language == "python" {
		return ConfidenceMedium
	}

	// WASM bindings: exported to JavaScript via syscall/js or promise wrappers.
	if isWASMPath(sym.FilePath) {
		return ConfidenceLow
	}

	// CLI command handlers in cmd/: wired via cobra.Command.RunE, not direct calls.
	if strings.HasPrefix(sym.FilePath, "cmd/") {
		// runXxx functions are cobra RunE callbacks — definitely alive.
		if strings.HasPrefix(sym.DisplayName, "run") && len(sym.DisplayName) > 3 &&
			sym.DisplayName[3] >= 'A' && sym.DisplayName[3] <= 'Z' {
			return ConfidenceLow
		}
		// completeXxx functions are cobra completion callbacks.
		if strings.HasPrefix(sym.DisplayName, "complete") && len(sym.DisplayName) > 8 &&
			sym.DisplayName[8] >= 'A' && sym.DisplayName[8] <= 'Z' {
			return ConfidenceLow
		}
		// Other cmd/ functions: hard to verify without running cobra wiring.
		return ConfidenceMedium
	}

	// WASM shell commands: registered via command map in pkg/wasmshell/.
	if strings.HasPrefix(sym.FilePath, "pkg/wasmshell/") {
		return ConfidenceLow
	}

	// In a registration file AND has a registration name → low confidence.
	if isRegistrationFile(sym.FilePath) && isRegistrationName(sym.DisplayName) {
		return ConfidenceLow
	}

	// In a registration file OR has a registration name → medium confidence.
	if isRegistrationFile(sym.FilePath) || isRegistrationName(sym.DisplayName) {
		return ConfidenceMedium
	}

	// Unexported Go function in a non-registration file → high confidence.
	return ConfidenceHigh
}

// isWASMPath returns true if the file path is in a WASM/JS-export directory.
func isWASMPath(path string) bool {
	return strings.HasPrefix(path, "cmd/wasm/") ||
		strings.HasPrefix(path, "cmd/embedding-wasm/") ||
		strings.HasPrefix(path, "cmd/model_registry_server/")
}

// FindDeadCodeWithMeta returns dead code candidates with confidence scoring
// and test-only caller detection. Test-only detection requires test files to
// be indexed in the graph; if they're not, TestCallers will always be 0.
//
// Use HasTestCallers() to do a filesystem-based check for test references
// on candidates where TestCallers is 0 but the graph may not include tests.
func (s *SQLiteStore) FindDeadCodeWithMeta(ctx context.Context, directory string) ([]DeadCodeCandidate, error) {
	symbols, err := s.FindDeadCode(ctx, directory)
	if err != nil {
		return nil, err
	}

	candidates := make([]DeadCodeCandidate, len(symbols))
	for i, sym := range symbols {
		// Check for test-file callers in the graph (if test files were indexed).
		testCallers, err := s.countTestCallers(ctx, sym.QualifiedName)
		if err != nil {
			testCallers = 0 // best-effort
		}

		candidates[i] = DeadCodeCandidate{
			Symbol:      sym,
			Confidence:  classifyConfidence(sym),
			TestCallers: testCallers,
		}
	}

	return candidates, nil
}

// countTestCallers returns the number of test-file callers for a symbol.
// Test files must be indexed in the graph for this to return non-zero.
func (s *SQLiteStore) countTestCallers(ctx context.Context, qualifiedName string) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var count int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(DISTINCT n.id)
		FROM nodes n
		JOIN edges e ON e.source_node_id = n.id
		JOIN nodes target ON e.target_node_id = target.id
		JOIN files f ON f.path = n.file_path
		WHERE target.qualified_name = ?
		AND (e.edge_type = 'resolved_calls' OR e.edge_type = 'calls')
		AND f.is_test = 1
	`, qualifiedName).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
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
//
// CAUTION: This is a heuristic lower-bound. Static call-graph extraction cannot
// trace through reflection, interface dispatch, cobra/click command registrations,
// or closures assigned to struct fields. Functions reported as "dead" may be
// reachable through these dynamic mechanisms. Treat results as candidates for
// manual review, not authoritative dead code.
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
		// Escape LIKE wildcards in the directory path to prevent them from
		// being interpreted as pattern matchers (e.g. a directory literally
		// containing "_" would otherwise match any single character).
		escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(directory)
		query += ` AND n.file_path LIKE ? ESCAPE '\'`
		args = append(args, escaped+"/%")
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
		if isEntryPointOrExcluded(sym) {
			continue
		}
		symbols = append(symbols, sym)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration error: %w", err)
	}

	return symbols, nil
}

// isEntryPointOrExcluded returns true for symbols that should never appear in
// dead-code results: Go entry points, methods (whose call graph depends on the
// receiver type's usage), and test infrastructure.
func isEntryPointOrExcluded(sym Symbol) bool {
	// Skip methods: Go methods have parenthesized receiver syntax like
	// "pkg.(*Type).Method" or "pkg.Type.Method" (more than one dot segment
	// after the last "/"). These are excluded because their inbound edges
	// depend on receiver-type usage, which static extraction under-resolves.
	lastSlash := strings.LastIndex(sym.QualifiedName, "/")
	afterSlash := sym.QualifiedName
	if lastSlash >= 0 {
		afterSlash = sym.QualifiedName[lastSlash+1:]
	}
	if strings.Contains(afterSlash, "(") || strings.Count(afterSlash, ".") > 1 {
		return true
	}
	// Skip exported Go functions (uppercase first char). These form the
	// package's public API and may be called from other packages, test
	// files, or via reflection — the extractor cannot determine reachability.
	if len(sym.DisplayName) > 0 && sym.DisplayName[0] >= 'A' && sym.DisplayName[0] <= 'Z' {
		return true
	}
	// Skip Go test/benchmark/fuzz entry points.
	if strings.HasPrefix(sym.DisplayName, "Test") ||
		strings.HasPrefix(sym.DisplayName, "Benchmark") ||
		strings.HasPrefix(sym.DisplayName, "Fuzz") {
		return true
	}
	// Skip init() and main().
	if sym.DisplayName == "init" || sym.DisplayName == "main" {
		return true
	}
	return false
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
