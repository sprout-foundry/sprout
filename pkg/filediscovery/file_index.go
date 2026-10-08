//go:build !js

package filediscovery

import (
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// IndexEntry is one row of the bulk file index consumed by the command
// palette's quick-open.
type IndexEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Type string `json:"type"`
}

// IndexResult reports the walked index plus whether caps truncated it.
type IndexResult struct {
	Files       []IndexEntry `json:"files"`
	Truncated   bool         `json:"truncated"`
	FileCount   int          `json:"file_count"`
	DirCount    int          `json:"dir_count"`
	SkippedDirs []string     `json:"skipped_dirs,omitempty"`
}

// IndexLimits bounds a workspace walk so a pathological tree cannot pin the
// server. Defaults match the frontend's previous crawl caps.
type IndexLimits struct {
	MaxFiles     int
	MaxDirs      int
	MaxDepth     int
	SkipDirNames map[string]bool
}

// DefaultIndexLimits mirrors the palette's historical crawl bounds.
func DefaultIndexLimits() IndexLimits {
	return IndexLimits{
		MaxFiles:     12000,
		MaxDirs:      3000,
		MaxDepth:     8,
		SkipDirNames: DefaultSkipDirectories(),
	}
}

// BuildFileIndex walks root once and returns every non-ignored file
// (workspace-relative slash paths) up to the limits. Directories matched by
// the workspace's ignore rules are pruned wholesale; directories in
// SkipDirNames are counted as skipped so the UI can explain a truncated
// index. Index is relative-path based: files carry no absolute paths, so an
// index built for one workspace can never leak rows into another.
// `.git` is always pruned.
func BuildFileIndex(root string, limits IndexLimits) IndexResult {
	if limits.MaxFiles <= 0 {
		limits.MaxFiles = DefaultIndexLimits().MaxFiles
	}
	if limits.MaxDirs <= 0 {
		limits.MaxDirs = DefaultIndexLimits().MaxDirs
	}
	if limits.MaxDepth <= 0 {
		limits.MaxDepth = DefaultIndexLimits().MaxDepth
	}
	if limits.SkipDirNames == nil {
		limits.SkipDirNames = DefaultSkipDirectories()
	}

	result := IndexResult{Files: []IndexEntry{}}
	rules := GetIgnoreRules(root)
	visitedDirs := 0

	var walk func(relDir string, depth int) bool
	walk = func(relDir string, depth int) bool {
		// Return true = keep walking, false = stop (caps hit).
		if result.FileCount >= limits.MaxFiles || visitedDirs >= limits.MaxDirs {
			result.Truncated = true
			return false
		}
		visitedDirs++

		absDir := filepath.Join(root, relDir)
		entries, err := os.ReadDir(absDir)
		if err != nil {
			return true
		}

		// Deterministic order regardless of filesystem enumeration.
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

		for _, entry := range entries {
			name := entry.Name()
			rel := name
			if relDir != "." {
				rel = relDir + "/" + name
			}

			if entry.IsDir() {
				if depth >= limits.MaxDepth {
					continue
				}
				if limits.SkipDirNames[name] {
					result.SkippedDirs = append(result.SkippedDirs, rel)
					continue
				}
				if rules != nil && rules.MatchesPath(rel+"/") {
					continue
				}
				if !walk(rel, depth+1) {
					return false
				}
				if result.Truncated {
					return false
				}
				continue
			}

			if result.FileCount >= limits.MaxFiles {
				result.Truncated = true
				return false
			}
			if rules != nil && rules.MatchesPath(rel) {
				continue
			}
			result.Files = append(result.Files, IndexEntry{Name: name, Path: normalizeIndexRelPath(rel), Type: "file"})
			result.FileCount++
		}
		return true
	}

	walk(".", 0)

	result.DirCount = visitedDirs
	return result
}

// DefaultSkipDirectories returns the directory names the index never descends
// into: build output, dependency stores, VCS internals, and OS/profile
// folders that are never project sources. Keeping this in pkg/filediscovery
// (not the webui handler) lets the WASM shell mirror the exact same list.
func DefaultSkipDirectories() map[string]bool {
	return map[string]bool{
		".git":          true,
		"node_modules":  true,
		".next":         true,
		".nuxt":         true,
		".svelte-kit":   true,
		"dist":          true,
		"build":         true,
		"out":           true,
		"__pycache__":   true,
		".venv":         true,
		"venv":          true,
		"vendor":        true,
		"target":        true,
		".turbo":        true,
		".cache":        true,
		".parcel-cache": true,
		"coverage":      true,
		".gradle":       true,
		".idea":         true,
		".vs":           true,
		// TCC-protected / never-a-project home folders (macOS, XDG, Windows
		// profile, cloud-sync roots): listing these raises macOS privacy
		// prompts when the workspace resolves to $HOME.
		"Library":      true,
		"Applications": true,
		"Documents":    true,
		"Desktop":      true,
		"Downloads":    true,
		"Pictures":     true,
		"Music":        true,
		"Movies":       true,
		"Public":       true,
		"Videos":       true,
		"Templates":    true,
		"AppData":      true,
		"OneDrive":     true,
		"Contacts":     true,
		"Favorites":    true,
		"Links":        true,
		"Saved Games":  true,
		"Searches":     true,
		"Dropbox":      true,
		"Google Drive": true,
		"iCloud Drive": true,
	}
}

// normalizeIndexRelPath keeps paths slash-separated for the JSON surface.
func normalizeIndexRelPath(rel string) string {
	return path.Clean(strings.ReplaceAll(rel, string(os.PathSeparator), "/"))
}
