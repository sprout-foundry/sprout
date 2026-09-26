package tools

// Package tools: repo_map orchestration - high-level directory walking and output formatting (split from original repo_map.go).

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/filesystem"
)

// repo_map.go — the repo_map walking core: the exported entry points
// and the filesystem walk that produces the map. Output formatting and the
// codegraph fallback live in repo_map_output.go. Pure move.

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
	".h": true, ".hpp": true, ".cc": true, ".cxx": true,
	".kt": true, ".kts": true,
	".swift": true,
	".cs":    true,
	".rb":    true,
	".m":     true, ".mm": true,
	".scala": true, ".sbt": true,
	".lua":  true,
	".php":  true,
	".dart": true,
	".ex":   true, ".exs": true,
	".clj": true, ".cljs": true, ".cljc": true,
	".sh": true, ".bash": true, ".zsh": true,
}

var ignoredDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, "dist": true,
	"build": true, ".next": true, "coverage": true, ".cache": true, ".sprout": true,
}

// isIgnoredDir checks if a directory should be skipped during walks.
// Uses the canonical shared list from pkg/filesystem so repo_map stays
// in sync with embedding and codegraph exclusion behavior.
func isIgnoredDir(name string) bool {
	if ignoredDirs[name] {
		return true
	}
	return filesystem.IsSkipDir(name)
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
	return GenerateRepoMapWithSemanticMatches(ctx, rootDir, depth, query, nil)
}

// GenerateRepoMapWithSemanticMatches is GenerateRepoMap with an optional set of
// workspace-relative paths that a semantic search matched for the same query.
//
// The plain query filter is a case-insensitive substring match on path and
// symbol name, so it can only answer questions where the caller already knows
// the identifier. "Show me the map, filtered to what matters for authentication"
// is exactly what an agent wants before opening files, and exactly what
// substring matching cannot do.
//
// The semantic set is passed in rather than resolved here so this function
// stays usable — and testable — with no embedding index present, and so the
// caller controls the cost. Matches are UNIONed with the substring matches:
// semantic recall is imperfect, so it should widen the map, never narrow it.
func GenerateRepoMapWithSemanticMatches(ctx context.Context, rootDir string, depth int, query string, semanticPaths map[string]bool) (string, error) {
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
	return generateRepoMapFromFS(ctx, absRoot, depth, query, semanticPaths)
}

// fileEntry is the per-file record produced by the repo-map walk. Hoisted to
// package scope so buildInclusionOrder can reuse the type.
type fileEntry struct {
	absPath, relPath, ext string
	depth                 int
}

func generateRepoMapFromFS(ctx context.Context, absRoot string, depth int, query string, semanticPaths map[string]bool) (string, error) {

	allFiles := make([]fileEntry, 0, 4096)
	// Non-source files and subdirectories, kept for the ls-style fallback when
	// the mapped tree contains no recognized source files at all.
	otherFiles := make([]fileEntry, 0, 256)
	subDirs := make([]string, 0, 64)
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
			if isIgnoredDir(name) {
				return filepath.SkipDir
			}
			if path != absRoot && strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			if path != absRoot {
				if rel, relErr := filepath.Rel(absRoot, path); relErr == nil {
					subDirs = append(subDirs, filepath.ToSlash(rel))
				}
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(name))
		if !sourceExtensions[ext] {
			if rel, relErr := filepath.Rel(absRoot, path); relErr == nil {
				relSlash := filepath.ToSlash(rel)
				otherFiles = append(otherFiles, fileEntry{path, relSlash, ext, strings.Count(relSlash, "/")})
			}
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
			treeFiles = filterByQuery(allFiles, query, semanticPaths)
		}
		var sb strings.Builder
		writeRepoMapHeader(&sb, absRoot, len(treeFiles), byExt, 0, 0)
		if conceptSummary != "" {
			sb.WriteString(conceptSummary)
		}
		sb.WriteString(formatDirectoryTree(absRoot, treeFiles))
		if len(treeFiles) == 0 {
			if ls := formatLSFallback(otherFiles, subDirs, query); ls != "" {
				sb.WriteString(ls)
			} else {
				sb.WriteString("\n*No source files found.*\n")
			}
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

		// Query filter: a file is included if its path matches the query, or a
		// semantic search matched it, (in which case all symbols are shown) or
		// if any of its symbols match the query (in which case only matching
		// symbols are shown).
		//
		// A semantic match keeps the whole file: the point of asking
		// conceptually is that the caller does not know which identifier to
		// look for, so filtering that file's symbols by the same literal string
		// would discard exactly what was just found.
		if query != "" && !semanticPaths[f.relPath] {
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
		if totalSourceFileCount == 0 {
			if ls := formatLSFallback(otherFiles, subDirs, query); ls != "" {
				sb.WriteString(ls)
				return sb.String(), nil
			}
		}
		sb.WriteString("\n*No source files with symbols found.*\n")
	}
	return sb.String(), nil
}
