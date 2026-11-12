package tools

import (
	"os"
	"path"
	"regexp"
	"sort"
	"strings"
)

// repoMapQueryMaxFiles bounds how many files a query parses for symbols:
// a common word can appear in hundreds of files, and parsing every one of
// them made a query take minutes.
const repoMapQueryMaxFiles = 120

// repoMapQueryFewMatches is how few files a query must match by path or
// declaration before file contents are searched for mere mentions too.
// Reading every file is what makes a query slow; a query that already
// matched this many files has what it needs.
const repoMapQueryFewMatches = 20

// declName finds what a line declares (a function, method, type, class,
// constant...) in the languages repo_map reads: the name is the last group.
// A matching symbol's name would be there.
var declName = regexp.MustCompile(`^\s*(?:export\s+)?(?:default\s+)?(?:pub(?:\(crate\))?\s+)?(?:async\s+)?(?:func|type|class|interface|struct|enum|const|var|let|def|fn|function|trait|module)\s+(?:\([^)]*\)\s*)?([A-Za-z_$][\w$]*)`)

// filterFilesByQueryContent picks the files a query can match before any cap
// applies, best first: those named for the query (admin finds Admin.tsx,
// "task queue" task_queue.go), then the rest whose path matches, then those that declare
// something matching, then those that only mention every query word; within
// each, code before tests, one package at a time.
// Filtering after the caps searched only the files the caps happened to keep.
//
// A file the code index holds current symbols for is matched on those
// symbols without being read; its contents are searched for mentions only
// when the path and declaration matches are few. At most
// repoMapQueryMaxFiles are kept.
func filterFilesByQueryContent(files []fileEntry, query string, index *repoMapIndex) []fileEntry {
	words := queryWords(query)
	if len(words) == 0 {
		return files
	}
	var byName, byPath, byDecl, byMention, unread []fileEntry
	for _, f := range files {
		if matchesQueryWords(f.relPath, words) {
			if isQueryName(f.relPath, words) {
				byName = append(byName, f)
			} else {
				byPath = append(byPath, f)
			}
			continue
		}
		if symbols, ok := index.symbolsFor(f); ok {
			if len(filterSymbolsByQuery(symbols, query)) > 0 {
				byDecl = append(byDecl, f)
			} else {
				unread = append(unread, f)
			}
			continue
		}
		text, ok := readQueryCandidate(f)
		if !ok || !matchesQueryWords(text, words) {
			continue
		}
		if declaresQueryWords(f.ext, text, query, words) {
			byDecl = append(byDecl, f)
		} else {
			byMention = append(byMention, f)
		}
	}
	if len(byName)+len(byPath)+len(byDecl) < repoMapQueryFewMatches {
		for _, f := range unread {
			if text, ok := readQueryCandidate(f); ok && matchesQueryWords(text, words) {
				byMention = append(byMention, f)
			}
		}
	}
	groupOf := capGroups(files)
	var result []fileEntry
	for _, list := range [][]fileEntry{byName, byPath, byDecl, byMention} {
		// Within a rank, the code before its tests.
		var code, tests []fileEntry
		for _, f := range list {
			if isTestFile(f.relPath) {
				tests = append(tests, f)
			} else {
				code = append(code, f)
			}
		}
		result = append(result, roundRobinByGroup(code, groupOf)...)
		result = append(result, roundRobinByGroup(tests, groupOf)...)
	}
	if len(result) > repoMapQueryMaxFiles {
		result = result[:repoMapQueryMaxFiles]
	}
	return result
}

// roundRobinByGroup orders files one per group (capGroups) in turn, so the
// first matches shown span the repository instead of filling up with one
// package's files.
func roundRobinByGroup(files []fileEntry, groupOf func(string) string) []fileEntry {
	sorted := append([]fileEntry(nil), files...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].relPath < sorted[j].relPath })
	var groups []string
	byGroup := make(map[string][]fileEntry)
	for _, f := range sorted {
		g := groupOf(f.relPath)
		if _, ok := byGroup[g]; !ok {
			groups = append(groups, g)
		}
		byGroup[g] = append(byGroup[g], f)
	}
	out := make([]fileEntry, 0, len(files))
	for len(out) < len(files) {
		for _, g := range groups {
			if list := byGroup[g]; len(list) > 0 {
				out = append(out, list[0])
				byGroup[g] = list[1:]
			}
		}
	}
	return out
}

// isQueryName reports whether a file's name, without its extensions, is the
// query's words run together (ignoring case and _ - .).
func isQueryName(relPath string, words []string) bool {
	base := path.Base(relPath)
	if i := strings.Index(base, "."); i > 0 {
		base = base[:i]
	}
	folded := strings.NewReplacer("_", "", "-", "").Replace(strings.ToLower(base))
	return folded == strings.Join(words, "")
}

// readQueryCandidate reads a file a query may match, skipping one too large
// for the map.
func readQueryCandidate(f fileEntry) (string, bool) {
	info, err := os.Stat(f.absPath)
	if err != nil || info.Size() > repoMapMaxFullFileSize {
		return "", false
	}
	content, err := os.ReadFile(f.absPath)
	if err != nil {
		return "", false
	}
	return string(content), true
}

// declaresQueryWords reports whether a file's text declares a name holding
// every query word. Languages repo_map has no parser for are read with their
// regex extractor, the same symbols the map lists for them; the rest with
// declName.
func declaresQueryWords(ext, text, query string, words []string) bool {
	if ext != ".go" && !treeSitterExtensions[ext] {
		return len(filterSymbolsByQuery(extractSymbolsViaRegex([]byte(text), ext), query)) > 0
	}
	for _, line := range strings.Split(text, "\n") {
		if m := declName.FindStringSubmatch(line); m != nil && matchesQueryWords(m[1], words) {
			return true
		}
	}
	return false
}
