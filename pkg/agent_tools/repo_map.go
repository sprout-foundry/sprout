package tools

// Package tools: repo_map orchestration - high-level directory walking and output formatting (split from original repo_map.go).

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	codegraph "github.com/sprout-foundry/sprout/pkg/codegraph"
	"github.com/sprout-foundry/sprout/pkg/filesystem"
)

const (
	repoMapMaxFullFileSize   = 2 * 1024 * 1024 // 2MB max file size
	repoMapTokenBudget       = 4096            // target ~4096 tokens (~16k chars) — raised from 1024 to cover repos with thousands of source files
	repoMapMaxFiles          = 2000            // cap on files to surface — raised from 200; together with the depth-aware prioritization this prevents one mega-directory from starving the rest
	repoMapCharBudget        = repoMapTokenBudget * 4
	repoMapMaxDepth          = 8        // cap walking depth so deeply-nested vendored trees don't dominate the budget
	repoMapRootFileAllowance = 64       // number of root-level files/dirs to keep before L1 takes over
	repoMapPerDirCap         = 60       // max files shown per directory (prevents pkg/foo/ from hogging the whole output)
	repoMapPerDirChars       = 8 * 1024 // max chars spent per directory in the formatted output

	// Depth levels for the repo map.
	depthDirTreeOnly = 1 // directory tree with file counts, no symbols
	depthTopSymbols  = 2 // tree + symbols for root-level and top-level files only (max 15 symbols/file)
	depthFullSymbols = 3 // full symbol listing (current behavior)

	// For depth=2, maximum symbols extracted per file.
	depth2MaxSymbolsPerFile = 15
	// For depth=2, only extract symbols from files at depth <= 1 (root + first level).
	depth2MaxFileDepth = 1
)

var sourceExtensions = map[string]bool{
	".go": true, ".ts": true, ".tsx": true, ".js": true, ".jsx": true,
	".py": true, ".rs": true, ".java": true, ".c": true, ".cpp": true,
	".h": true,
}

var ignoredDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, "dist": true,
	"build": true, ".next": true, "coverage": true, ".cache": true, ".sprout": true,
}

// GenerateRepoMap walks the directory tree rooted at rootDir and produces a
// lightweight overview of the codebase showing file paths and top-level symbols.
// For Go files it uses go/ast; for TS/JS/Python it uses tree-sitter via pkg/ast.
//
// depth controls the detail level:
//   - 1: directory tree with file counts per dir, no symbols
//   - 2: directory tree + symbols in root-level and top-level files only (max 15 symbols per file)
//   - 3 (default): full symbol listing
//
// query, when non-empty, filters files to only those whose path or symbol
// names contain the query string (case-insensitive).
//
// When the codegraph store is available and populated, it reads from the store
// for near-instant results on warm cache, falling back to the filesystem walk.
func GenerateRepoMap(ctx context.Context, rootDir string, depth int, query string) (string, error) {
	if depth <= 0 {
		depth = depthFullSymbols
	}
	query = strings.TrimSpace(query)
	if rootDir == "" || rootDir == "." {
		// Use the workspace root from context (set by withToolExecutionMetadata)
		// instead of os.Getwd(), which returns the daemon's CWD, not the
		// workspace the agent is operating in.
		if wsRoot := filesystem.WorkspaceRootFromContext(ctx); wsRoot != "" {
			rootDir = wsRoot
		} else {
			var err error
			rootDir, err = os.Getwd()
			if err != nil {
				return "", fmt.Errorf("get working directory: %w", err)
			}
		}
	}

	absRoot, err := filepath.Abs(rootDir)
	if err != nil {
		return "", fmt.Errorf("resolve root directory: %w", err)
	}

	// Try to use the codegraph store for instant results on warm cache.
	// Only use the store when the requested rootDir is the git root
	// (store.baseDir); otherwise fall through to filesystem walk.
	// The store path does not support depth filtering, so it is only used
	// for depth=3 with no query filter.
	if depth == depthFullSymbols && query == "" {
		store, storeErr := openGraphStore()
		if storeErr == nil && store != nil {
			defer store.Close()

			// Check that absRoot matches the store's baseDir so we don't
			// return project-wide data for a subdirectory query.
			storeAbsBase, err := filepath.Abs(store.BaseDir())
			if err == nil && storeAbsBase == absRoot {
				stats := store.Stats()
				if stats.FileCount > 0 {
					nodes, queryErr := store.QueryAllNodes(ctx)
					if queryErr == nil {
						result := formatRepoMapFromNodes(absRoot, nodes)
						if result != "" {
							return result, nil
						}
					}
				}
			}
		}
	}

	// Fall through to filesystem walk.
	return generateRepoMapFromFS(ctx, absRoot, depth, query)
}

// fileEntry is the per-file record produced by the repo-map walk. Hoisted to
// package scope so buildInclusionOrder can reuse the type.
type fileEntry struct {
	absPath, relPath, ext string
	depth                 int
}

func generateRepoMapFromFS(ctx context.Context, absRoot string, depth int, query string) (string, error) {

	allFiles := make([]fileEntry, 0, 4096)
	walkErr := walkDirCompat(absRoot, func(path string, d os.DirEntry, err error) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if err != nil {
			return nil
		}
		name := d.Name()
		// Skip symlinks to prevent following links outside the target tree.
		if d.Type()&os.ModeSymlink != 0 {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if ignoredDirs[name] {
				return filepath.SkipDir
			}
			if path != absRoot && strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(name))
		if !sourceExtensions[ext] {
			return nil
		}
		rel, err := filepath.Rel(absRoot, path)
		if err != nil {
			return nil
		}
		relSlash := filepath.ToSlash(rel)
		fdepth := strings.Count(relSlash, "/")
		if path == absRoot {
			fdepth = -1 // sentinel: never occurs (the walker doesn't call us for the root path itself)
		}
		allFiles = append(allFiles, fileEntry{path, relSlash, ext, fdepth})
		return nil
	})
	if walkErr != nil {
		return "", fmt.Errorf("walk directory: %w", walkErr)
	}

	// Pre-compute stats for the summary header.
	totalSourceFileCount := len(allFiles)
	byExt := make(map[string]int)
	for _, f := range allFiles {
		byExt[f.ext]++
	}

	// Build concept summary and entry points from the full file list before
	// any depth-based truncation, so all depth levels get this information.
	conceptSummary := formatConceptSummary(allFiles)

	// --- Depth 1: directory tree only, no symbols ---
	if depth == depthDirTreeOnly {
		// Apply query filter to the file list for the tree.
		treeFiles := allFiles
		if query != "" {
			treeFiles = filterByQuery(allFiles, query)
		}
		var sb strings.Builder
		writeRepoMapHeader(&sb, absRoot, len(treeFiles), byExt, 0, 0)
		if conceptSummary != "" {
			sb.WriteString(conceptSummary)
		}
		sb.WriteString(formatDirectoryTree(absRoot, treeFiles))
		if len(treeFiles) == 0 {
			sb.WriteString("\n*No source files found.*\n")
		}
		return sb.String(), nil
	}

	// Build the inclusion order: root, then round-robin across L1 dirs, then
	// round-robin across deeper levels with caps applied per-directory.
	ordered, dirsCovered, dirsOmitted := buildInclusionOrder(allFiles)

	// Apply the file-count cap as a safety net.
	if len(ordered) > repoMapMaxFiles {
		ordered = ordered[:repoMapMaxFiles]
	}

	var sb strings.Builder
	charCount := 0
	fileCount := 0
	truncated := false
	truncationReason := ""

	writeHeader := func() {
		sb.WriteString("## repo_map: ")
		sb.WriteString(filepath.Base(absRoot))
		sb.WriteString("\n")
		// Build a sorted ext list for deterministic output.
		exts := make([]string, 0, len(byExt))
		for e := range byExt {
			exts = append(exts, e)
		}
		sort.Strings(exts)
		extParts := make([]string, 0, len(exts))
		for _, e := range exts {
			extParts = append(extParts, fmt.Sprintf("%s: %d", e, byExt[e]))
		}
		fmt.Fprintf(&sb, "- total source files: %d (%s)\n", totalSourceFileCount, strings.Join(extParts, ", "))
		if totalSourceFileCount > 0 {
			fmt.Fprintf(&sb, "- dirs covered: %d\n", dirsCovered)
			if dirsOmitted > 0 {
				fmt.Fprintf(&sb, "- dirs omitted (file cap reached before they could be sampled): %d\n", dirsOmitted)
			}
		}
		// Add concept summary and entry points for all depth levels >= 2.
		if conceptSummary != "" {
			sb.WriteString(conceptSummary)
		}
		charCount = sb.Len()
	}

	writeHeader()

	// Track which top-level dirs had files emitted into the output.
	emittedDirs := make(map[string]bool)
	// Track all top-level dirs that have files (for better truncation messages).
	allTopDirs := make(map[string]bool)
	for _, f := range ordered {
		allTopDirs[topDir(f.relPath)] = true
	}

	perDirChars := make(map[string]int)
	emittedPerDir := make(map[string]int)

	for _, f := range ordered {
		select {
		case <-ctx.Done():
			truncated = true
			truncationReason = "context cancelled"
			break
		default:
		}

		dir := topDir(f.relPath)
		// Per-directory caps.
		if emittedPerDir[dir] >= repoMapPerDirCap {
			continue
		}
		if perDirChars[dir] >= repoMapPerDirChars {
			continue
		}

		// Depth-2: skip files deeper than depth2MaxFileDepth.
		if depth == depthTopSymbols && f.depth > depth2MaxFileDepth {
			continue
		}

		content, readErr := os.ReadFile(f.absPath)
		if readErr != nil {
			continue
		}
		if len(content) > repoMapMaxFullFileSize {
			continue
		}
		if isBinaryContent(content) {
			continue
		}

		symbols, err := extractSymbolsForFile(f.absPath, f.ext, content)
		if err != nil {
			continue
		}

		// Depth-2: cap symbols per file.
		if depth == depthTopSymbols && len(symbols) > depth2MaxSymbolsPerFile {
			symbols = symbols[:depth2MaxSymbolsPerFile]
		}

		// Query filter: a file is included if its path matches the query
		// (in which case all symbols are shown) or if any of its symbols
		// match the query (in which case only matching symbols are shown).
		if query != "" {
			if !strings.Contains(strings.ToLower(f.relPath), strings.ToLower(query)) {
				// Path doesn't match — filter at symbol level.
				symbols = filterSymbolsByQuery(symbols, query)
			}
		}

		if len(symbols) == 0 {
			continue
		}

		// Render symbols with one per line, prefix and line separated by a
		// space-then-colon. Same shape as before, just consistent.
		var sectionSB strings.Builder
		sectionSB.WriteString("\n### ")
		sectionSB.WriteString(f.relPath)
		sectionSB.WriteString("\n")
		for _, sym := range symbols {
			fmt.Fprintf(&sectionSB, "- %s:%d\n", sym.Name, sym.Line)
		}
		section := sectionSB.String()

		// Honor the global char budget, but always include the first file
		// we see so we never return an empty map.
		if charCount+len(section) > repoMapCharBudget && fileCount > 0 {
			truncated = true
			truncationReason = "char budget reached"
			break
		}
		sb.WriteString(section)
		charCount += len(section)
		fileCount++
		emittedPerDir[dir]++
		perDirChars[dir] += len(section)
		emittedDirs[dir] = true
	}

	if truncated {
		// Build improved truncation message listing omitted top-level dirs.
		var omittedDirNames []string
		for d := range allTopDirs {
			if !emittedDirs[d] {
				omittedDirNames = append(omittedDirNames, d)
			}
		}
		sort.Strings(omittedDirNames)
		if len(omittedDirNames) > 5 {
			omittedDirNames = omittedDirNames[:5]
		}

		omittedStr := ""
		if len(omittedDirNames) > 0 {
			omittedStr = fmt.Sprintf(" Omitted: %s.", strings.Join(omittedDirNames, ", "))
		}
		suggestion := ""
		if len(omittedDirNames) > 0 {
			suggestion = fmt.Sprintf(" Try: repo_map directory=%s to drill into specific areas.", omittedDirNames[0])
		}
		fmt.Fprintf(&sb, "\n*... truncated (%s); output covers %d of %d files (%.0f%%), %d dirs.%s%s*\n",
			truncationReason,
			fileCount,
			totalSourceFileCount,
			pct(fileCount, totalSourceFileCount),
			dirsCovered,
			omittedStr,
			suggestion)
	}
	if fileCount == 0 {
		sb.WriteString("\n*No source files with symbols found.*\n")
	}
	return sb.String(), nil
}

// buildInclusionOrder groups source files by their top-level directory and
// emits them in a priority order designed to give every top-level area some
// representation: root files first (up to repoMapRootFileAllowance), then a
// round-robin across the L1 directories, with each directory capped at
// repoMapPerDirCap files. Within a directory, files are sorted alphabetically
// for deterministic output.
//
// Returns the ordered list, the number of distinct directories represented,
// and the number of directories that were entirely omitted (had files but
// were beyond the cap).
func buildInclusionOrder(files []fileEntry) (ordered []fileEntry, dirsRepresented int, dirsOmitted int) {

	// Root files: relPath has no slash.
	root := make([]fileEntry, 0, 16)
	// L1 dir -> sorted file list.
	byDir := make(map[string][]fileEntry)
	for _, f := range files {
		if !strings.Contains(f.relPath, "/") {
			root = append(root, f)
			continue
		}
		dir := topDir(f.relPath)
		byDir[dir] = append(byDir[dir], f)
	}

	// Sort root alphabetically and apply cap.
	sort.Slice(root, func(i, j int) bool { return root[i].relPath < root[j].relPath })
	if len(root) > repoMapRootFileAllowance {
		root = root[:repoMapRootFileAllowance]
	}
	ordered = append(ordered, root...)

	// Sort per-dir lists alphabetically; track which dirs are represented vs. omitted.
	dirNames := make([]string, 0, len(byDir))
	for d := range byDir {
		dirNames = append(dirNames, d)
	}
	sort.Strings(dirNames)

	// Cap how many files we pull per dir before bailing on per-dir cycle.
	perDirLimit := repoMapPerDirCap

	for _, d := range dirNames {
		entries := byDir[d]
		sort.Slice(entries, func(i, j int) bool { return entries[i].relPath < entries[j].relPath })
		take := len(entries)
		if take > perDirLimit {
			take = perDirLimit
			dirsOmitted++ // the dir had files beyond the cap; flag it as partly omitted
		}
		ordered = append(ordered, entries[:take]...)
		dirsRepresented++
	}

	return ordered, dirsRepresented, dirsOmitted
}

// topDir returns the first path component of a slash-separated relative path,
// or "" if the path has no slash (i.e. it's a root-level file).
func topDir(relPath string) string {
	if idx := strings.IndexByte(relPath, '/'); idx >= 0 {
		return relPath[:idx]
	}
	return ""
}

func pct(num, denom int) float64 {
	if denom == 0 {
		return 0
	}
	return float64(num) / float64(denom) * 100
}

// writeRepoMapHeader writes the standard repo map header (title + file stats +
// dir coverage) into the provided string builder.
func writeRepoMapHeader(sb *strings.Builder, absRoot string, totalFiles int, byExt map[string]int, dirsCovered, dirsOmitted int) {
	sb.WriteString("## repo_map: ")
	sb.WriteString(filepath.Base(absRoot))
	sb.WriteString("\n")
	exts := make([]string, 0, len(byExt))
	for e := range byExt {
		exts = append(exts, e)
	}
	sort.Strings(exts)
	extParts := make([]string, 0, len(exts))
	for _, e := range exts {
		extParts = append(extParts, fmt.Sprintf("%s: %d", e, byExt[e]))
	}
	fmt.Fprintf(sb, "- total source files: %d (%s)\n", totalFiles, strings.Join(extParts, ", "))
	if totalFiles > 0 && dirsCovered > 0 {
		fmt.Fprintf(sb, "- dirs covered: %d\n", dirsCovered)
		if dirsOmitted > 0 {
			fmt.Fprintf(sb, "- dirs omitted (file cap reached before they could be sampled): %d\n", dirsOmitted)
		}
	}
}

// formatDirectoryTree produces a compact directory tree showing file counts
// per top-level directory. Used for depth=1 output.
func formatDirectoryTree(absRoot string, allFiles []fileEntry) string {
	if len(allFiles) == 0 {
		return ""
	}

	// Count files per top-level directory.
	rootFileCount := 0
	dirCounts := make(map[string]int)
	for _, f := range allFiles {
		td := topDir(f.relPath)
		if td == "" {
			rootFileCount++
		} else {
			dirCounts[td]++
		}
	}

	var sb strings.Builder
	sb.WriteString("\n### Directory Tree\n")

	if rootFileCount > 0 {
		fmt.Fprintf(&sb, "- / (%d files)\n", rootFileCount)
	}

	// Sort dirs alphabetically.
	dirNames := make([]string, 0, len(dirCounts))
	for d := range dirCounts {
		dirNames = append(dirNames, d)
	}
	sort.Strings(dirNames)

	for _, d := range dirNames {
		fmt.Fprintf(&sb, "- %s/ (%d files)\n", d, dirCounts[d])
	}

	return sb.String()
}

// formatConceptSummary builds the "Structure" and "Entry points" sections
// from the full file list. It groups directories by concept and identifies
// entry-point files.
func formatConceptSummary(allFiles []fileEntry) string {
	if len(allFiles) == 0 {
		return ""
	}

	// Count files per top-level directory.
	dirCounts := make(map[string]int)
	for _, f := range allFiles {
		td := topDir(f.relPath)
		if td != "" {
			dirCounts[td]++
		}
	}

	// Group directories by concept.
	conceptDirs := make(map[string][]string) // concept -> sorted dir names
	for dir, count := range dirCounts {
		_ = count
		concept := getConceptForDir(dir)
		conceptDirs[concept] = append(conceptDirs[concept], dir)
	}

	var sb strings.Builder

	// Structure section.
	if len(conceptDirs) > 0 {
		// Build concept parts in a deterministic order.
		conceptOrder := []string{"UI", "Services", "Utilities", "Tests", "Config", "Core", "Other"}
		seen := make(map[string]bool)
		var parts []string
		for _, concept := range conceptOrder {
			dirs, ok := conceptDirs[concept]
			if !ok {
				continue
			}
			seen[concept] = true
			sort.Strings(dirs)
			// Sum file counts across dirs for this concept.
			totalCount := 0
			testCount := 0
			var testExamples []string
			for _, d := range dirs {
				totalCount += dirCounts[d]
				// Check if this is a test dir.
				if isTestDirName(d) {
					testCount += dirCounts[d]
					if len(testExamples) < 3 {
						testExamples = append(testExamples, d)
					}
				}
			}
			if concept == "Tests" {
				if len(testExamples) > 0 {
					parts = append(parts, fmt.Sprintf("Tests (%d files: %s/)", totalCount, strings.Join(testExamples, "/, ")+"/"))
				} else {
					parts = append(parts, fmt.Sprintf("Tests (%d files)", totalCount))
				}
			} else {
				parts = append(parts, fmt.Sprintf("%s (%d files in %s/)", concept, totalCount, strings.Join(dirs, "/, ")+"/"))
			}
		}
		// Handle any concepts not in conceptOrder.
		for concept, dirs := range conceptDirs {
			if seen[concept] {
				continue
			}
			sort.Strings(dirs)
			totalCount := 0
			for _, d := range dirs {
				totalCount += dirCounts[d]
			}
			parts = append(parts, fmt.Sprintf("%s (%d files in %s/)", concept, totalCount, strings.Join(dirs, "/, ")+"/"))
		}
		if len(parts) > 0 {
			fmt.Fprintf(&sb, "- Structure: %s\n", strings.Join(parts, ", "))
		}
	}

	// Entry points section.
	var entryPoints []string
	for _, f := range allFiles {
		if isEntryPoint(f.relPath) {
			entryPoints = append(entryPoints, f.relPath)
		}
	}
	if len(entryPoints) > 0 {
		// Deduplicate and sort.
		entryPoints = dedupStrings(entryPoints)
		// Limit to a reasonable number.
		if len(entryPoints) > 10 {
			entryPoints = entryPoints[:10]
		}
		fmt.Fprintf(&sb, "- Entry points: %s\n", strings.Join(entryPoints, ", "))
	}

	return sb.String()
}

// filterByQuery filters the file list to only those whose path contains the
// query string (case-insensitive). Symbol-level filtering is applied
// separately during extraction.
func filterByQuery(files []fileEntry, query string) []fileEntry {
	q := strings.ToLower(query)
	var result []fileEntry
	for _, f := range files {
		if strings.Contains(strings.ToLower(f.relPath), q) {
			result = append(result, f)
		}
	}
	return result
}

// filterSymbolsByQuery keeps only symbols whose name contains the query
// string (case-insensitive).
func filterSymbolsByQuery(symbols []SymbolEntry, query string) []SymbolEntry {
	q := strings.ToLower(query)
	var result []SymbolEntry
	for _, s := range symbols {
		if strings.Contains(strings.ToLower(s.Name), q) {
			result = append(result, s)
		}
	}
	return result
}

// openGraphStore opens the codegraph store at the default path (.sprout/codegraph.db).
// Returns nil, nil when the store is cleanly unavailable (file doesn't exist).
// Returns an error if the store exists but can't be opened.
func openGraphStore() (*codegraph.SQLiteStore, error) {
	dbPath, err := codegraph.DefaultDBPath()
	if err != nil {
		return nil, nil // can't resolve path, silently fall through
	}
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return nil, nil // no store yet, silently fall through
	}

	store, err := codegraph.NewStore(dbPath)
	if err != nil {
		return nil, fmt.Errorf("open codegraph store: %w", err)
	}

	return store, nil
}

// formatRepoMapFromNodes formats the store-backed node data into the same
// output format as the filesystem-walk version.
// The DisplayName field stores the bare name (e.g., "run", "MyType", "(*Handler).ServeHTTP")
// without a kind prefix. We reconstruct the prefix from sym.Kind so the output
// matches the filesystem-walk format (e.g., "- func run:10", "- type MyType:5").
func formatRepoMapFromNodes(rootDir string, nodes []codegraph.Symbol) string {
	if len(nodes) == 0 {
		return ""
	}

	// Group nodes by file_path.
	fileNodes := make(map[string][]codegraph.Symbol)
	for _, n := range nodes {
		fileNodes[n.FilePath] = append(fileNodes[n.FilePath], n)
	}

	// Sort file paths for deterministic output.
	filePaths := make([]string, 0, len(fileNodes))
	for p := range fileNodes {
		filePaths = append(filePaths, p)
	}
	sort.Strings(filePaths)

	var sb strings.Builder
	sb.WriteString("## repo_map: ")
	sb.WriteString(filepath.Base(rootDir))
	sb.WriteString("\n")

	charCount := sb.Len()
	fileCount := 0
	truncated := false

	for _, fp := range filePaths {
		syms := fileNodes[fp]

		section := "\n### " + fp + "\n"
		for _, sym := range syms {
			prefix := kindToPrefix(sym.Kind)
			section += fmt.Sprintf("- %s %s:%d\n", prefix, sym.DisplayName, sym.Line)
		}
		if charCount+len(section) > repoMapCharBudget && fileCount > 0 {
			truncated = true
			break
		}
		sb.WriteString(section)
		charCount += len(section)
		fileCount++
	}

	if truncated {
		sb.WriteString("\n*... truncated (token budget reached)*\n")
	}
	if fileCount == 0 {
		sb.WriteString("\n*No source files with symbols found.*\n")
	}

	return sb.String()
}
