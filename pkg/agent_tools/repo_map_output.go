package tools

// Package tools: repo_map orchestration - high-level directory walking and output formatting (split from original repo_map.go).

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	codegraph "github.com/sprout-foundry/sprout/pkg/codegraph"
)

// repo_map_output.go — the inclusion ordering, query filters, output
// formatting, and codegraph-fallback half of the repo map, split out of
// repo_map.go. Pure move.

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

// formatLSFallback renders an ls-style listing of the non-source files and
// subdirectories the walk collected. It is used only when the mapped tree has
// no recognized source files, so an agent pointing repo_map at a docs-only or
// config-only directory still sees its contents instead of an empty answer.
// When query is non-empty, only files whose path contains it are listed.
func formatLSFallback(otherFiles []fileEntry, subDirs []string, query string) string {
	if query != "" {
		otherFiles = filterByQuery(otherFiles, query, nil)
	}
	if len(otherFiles) == 0 && len(subDirs) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("\n### Contents (no source files; ls-style listing)\n")

	sortedDirs := append([]string(nil), subDirs...)
	sort.Strings(sortedDirs)
	for _, d := range sortedDirs {
		fmt.Fprintf(&sb, "- %s/\n", d)
	}
	sort.Slice(otherFiles, func(i, j int) bool { return otherFiles[i].relPath < otherFiles[j].relPath })
	shown := otherFiles
	if len(shown) > repoMapMaxFiles {
		shown = shown[:repoMapMaxFiles]
	}
	for _, f := range shown {
		fmt.Fprintf(&sb, "- %s\n", f.relPath)
	}
	if len(otherFiles) > len(shown) {
		fmt.Fprintf(&sb, "- *... %d more files (truncated)*\n", len(otherFiles)-len(shown))
	}

	if sb.Len() > repoMapCharBudget {
		trunc := sb.String()[:repoMapCharBudget]
		lastNL := strings.LastIndex(trunc, "\n")
		if lastNL > 0 {
			trunc = trunc[:lastNL]
		}
		sb.Reset()
		sb.WriteString(trunc)
		sb.WriteString("\n- *... listing truncated at char budget*\n")
	}

	return sb.String()
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
// filterByQuery keeps files whose path contains the query (case-insensitive)
// OR that a semantic search matched for the same query. The union matters: a
// substring hit is precise but literal, a semantic hit is conceptual but
// approximate, and dropping either shrinks the map an agent uses to decide what
// to read.
func filterByQuery(files []fileEntry, query string, semanticPaths map[string]bool) []fileEntry {
	q := strings.ToLower(query)
	var result []fileEntry
	for _, f := range files {
		if strings.Contains(strings.ToLower(f.relPath), q) || semanticPaths[f.relPath] {
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
