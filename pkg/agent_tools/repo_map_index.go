package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/codegraph"
)

// repoMapIndex is the code index's symbols for the files under a repo_map
// root, so the map can list a file's symbols without reading and parsing it.
// Only a file unchanged since it was indexed is served from here; any other
// file is parsed from disk as before. A nil index serves nothing.
type repoMapIndex struct {
	symbols   map[string][]SymbolEntry // by path relative to the map's root
	indexedAt map[string]time.Time
}

// loadRepoMapIndex reads the code index of the checkout holding absRoot, or
// returns nil when it has none (the background build has not finished, or
// the root is not in a git checkout).
func loadRepoMapIndex(ctx context.Context, absRoot string) *repoMapIndex {
	store, err := codegraph.OpenForDir(absRoot)
	if err != nil || store == nil {
		return nil
	}
	defer func() { _ = store.Close() }()

	// git reports the checkout's root with symlinks resolved.
	resolvedRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		return nil
	}
	prefix, err := filepath.Rel(store.BaseDir(), resolvedRoot)
	if err != nil || strings.HasPrefix(prefix, "..") {
		return nil
	}
	prefix = filepath.ToSlash(prefix)
	if prefix == "." {
		prefix = ""
	} else {
		prefix += "/"
	}
	under := func(path string) (string, bool) {
		if !strings.HasPrefix(path, prefix) {
			return "", false
		}
		return strings.TrimPrefix(path, prefix), true
	}

	times, err := store.FileIndexTimes(ctx)
	if err != nil || len(times) == 0 {
		return nil
	}
	nodes, err := store.QueryAllNodes(ctx)
	if err != nil {
		return nil
	}
	index := &repoMapIndex{
		symbols:   make(map[string][]SymbolEntry),
		indexedAt: make(map[string]time.Time, len(times)),
	}
	for path, at := range times {
		if rel, ok := under(path); ok {
			index.indexedAt[rel] = at
		}
	}
	for _, n := range nodes {
		if rel, ok := under(n.FilePath); ok {
			index.symbols[rel] = append(index.symbols[rel], SymbolEntry{
				Name: kindToPrefix(n.Kind) + " " + n.DisplayName,
				Line: n.Line,
			})
		}
	}
	return index
}

// symbolsFor returns the indexed symbols of a file, and false when the index
// does not hold the file or the file changed since it was indexed.
func (ix *repoMapIndex) symbolsFor(f fileEntry) ([]SymbolEntry, bool) {
	if ix == nil {
		return nil, false
	}
	at, ok := ix.indexedAt[f.relPath]
	if !ok {
		return nil, false
	}
	info, err := os.Stat(f.absPath)
	if err != nil || info.ModTime().After(at) {
		return nil, false
	}
	return ix.symbols[f.relPath], true
}
