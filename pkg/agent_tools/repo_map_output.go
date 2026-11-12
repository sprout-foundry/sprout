package tools

// Package tools: repo_map orchestration - high-level directory walking and output formatting (split from original repo_map.go).

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
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
	// Cap group -> sorted file list. A group is a top-level directory, or one
	// of its subdirectories when the top-level directory alone holds more
	// files than a group may show (capGroups).
	groupOf := capGroups(files)
	byDir := make(map[string][]fileEntry)
	for _, f := range files {
		if !strings.Contains(f.relPath, "/") {
			root = append(root, f)
			continue
		}
		g := groupOf(f.relPath)
		byDir[g] = append(byDir[g], f)
	}

	// Sort root alphabetically and apply cap.
	sort.Slice(root, func(i, j int) bool { return root[i].relPath < root[j].relPath })
	if len(root) > repoMapRootFileAllowance {
		root = root[:repoMapRootFileAllowance]
	}
	ordered = append(ordered, root...)

	dirNames := make([]string, 0, len(byDir))
	for d := range byDir {
		dirNames = append(dirNames, d)
		entries := byDir[d]
		sort.Slice(entries, func(i, j int) bool { return entries[i].relPath < entries[j].relPath })
		if len(entries) > repoMapPerDirCap {
			byDir[d] = entries[:repoMapPerDirCap]
			dirsOmitted++ // the group had files beyond the cap; flag it as partly omitted
		}
	}
	sort.Strings(dirNames)
	dirsRepresented = len(dirNames)

	// Round-robin across the groups: every group's first file, then every
	// group's second, and so on, so the output budget is spread over all of
	// them instead of being spent on the alphabetically first few.
	for i := 0; ; i++ {
		took := false
		for _, d := range dirNames {
			if i < len(byDir[d]) {
				ordered = append(ordered, byDir[d][i])
				took = true
			}
		}
		if !took {
			break
		}
	}

	return ordered, dirsRepresented, dirsOmitted
}

// capGroups returns the function that names the cap group of a file: its
// top-level directory, or, when that directory holds more files than one group
// may show, its first two path components (internal/api, src/components), so a
// repository whose code sits under one top-level directory still shows every
// package.
func capGroups(files []fileEntry) func(relPath string) string {
	perTop := make(map[string]int)
	for _, f := range files {
		perTop[topDir(f.relPath)]++
	}
	return func(relPath string) string {
		top := topDir(relPath)
		if perTop[top] <= repoMapPerDirCap {
			return top
		}
		rest := relPath[len(top)+1:]
		if idx := strings.IndexByte(rest, '/'); idx >= 0 {
			return top + "/" + rest[:idx]
		}
		return top
	}
}

// groupsOf returns the distinct cap groups the files fall in.
func groupsOf(files []fileEntry) map[string]bool {
	groupOf := capGroups(files)
	out := make(map[string]bool)
	for _, f := range files {
		out[groupOf(f.relPath)] = true
	}
	return out
}

// queryWords splits a repo_map query into lower-case words, so "feature
// review" finds FeatureReview, feature_review.go and featureReview.
func queryWords(query string) []string {
	return strings.FieldsFunc(strings.ToLower(query), func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9')
	})
}

// matchesQueryWords reports whether s contains every query word, ignoring
// case and the separators identifiers and paths use (_ - . /).
func matchesQueryWords(s string, words []string) bool {
	if len(words) == 0 {
		return false
	}
	folded := strings.NewReplacer("_", "", "-", "", ".", "", "/", "").Replace(strings.ToLower(s))
	for _, w := range words {
		if !strings.Contains(folded, w) {
			return false
		}
	}
	return true
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
		otherFiles = filterByQuery(otherFiles, query)
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
func filterByQuery(files []fileEntry, query string) []fileEntry {
	words := queryWords(query)
	var result []fileEntry
	for _, f := range files {
		if matchesQueryWords(f.relPath, words) {
			result = append(result, f)
		}
	}
	return result
}

// filterSymbolsByQuery keeps only symbols whose name contains every query
// word (case-insensitive, separators ignored).
func filterSymbolsByQuery(symbols []SymbolEntry, query string) []SymbolEntry {
	words := queryWords(query)
	var result []SymbolEntry
	for _, s := range symbols {
		if matchesQueryWords(s.Name, words) {
			result = append(result, s)
		}
	}
	return result
}
