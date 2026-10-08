//go:build js && wasm

// wasm_file_index.go — bulk quick-open index for the WASM shell.
//
// The palette's crawl used to cross the JS→WASM boundary once per directory
// (listDir JSON per call, hundreds of round-trips on real trees). This
// walks the whole workspace inside the WASM instance in one call and
// returns the index as a single JSON document — the same row shape the
// daemon's /api/file-index serves.

package wasmshell

import (
	"encoding/json"
	"path/filepath"
	"sort"
)

// wasmIndexLimits bound the walk; they mirror pkg/filediscovery's defaults
// so both surfaces cap identically. (filediscovery itself is not imported
// here to keep the WASM binary lean — the lists are small and stable.)
const (
	wasmIndexMaxFiles = 12000
	wasmIndexMaxDirs  = 3000
	wasmIndexMaxDepth = 8
)

var wasmSkipDirs = map[string]bool{
	".git": true, "node_modules": true, ".next": true, ".nuxt": true,
	".svelte-kit": true, "dist": true, "build": true, "out": true,
	"__pycache__": true, ".venv": true, "venv": true, "vendor": true,
	"target": true, ".turbo": true, ".cache": true, ".parcel-cache": true,
	"coverage": true, ".gradle": true, ".idea": true, ".vs": true,
	// TCC-protected / never-a-project home folders.
	"Library": true, "Applications": true, "Documents": true, "Desktop": true,
	"Downloads": true, "Pictures": true, "Music": true, "Movies": true,
	"Public": true, "Videos": true, "Templates": true, "AppData": true,
	"OneDrive": true, "Contacts": true, "Favorites": true, "Links": true,
	"Saved Games": true, "Searches": true, "Dropbox": true,
	"Google Drive": true, "iCloud Drive": true,
}

type wasmIndexEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Type string `json:"type"`
}

type wasmIndexResult struct {
	Files     []wasmIndexEntry `json:"files"`
	Truncated bool             `json:"truncated"`
	FileCount int              `json:"file_count"`
	DirCount  int              `json:"dir_count"`
}

// WalkFilesJSON walks the workspace root and returns the quick-open index
// as JSON: {"files":[{name,path,type}...],"truncated":bool,"file_count":n,
// "dir_count":n}. Paths are workspace-relative with forward slashes, so a
// stale client-side cache can never leak rows across a workspace switch.
// Hidden files are indexed (dotfiles are legitimate quick-open targets);
// skip-list directories are pruned. Uses WalkCompat for the js/wasm
// O_DIRECTORY workaround.
func WalkFilesJSON(root string) (string, error) {
	result := wasmIndexResult{Files: []wasmIndexEntry{}}
	visitedDirs := 0

	var walk func(absDir, relDir string, depth int) bool
	walk = func(absDir, relDir string, depth int) bool {
		if result.FileCount >= wasmIndexMaxFiles || visitedDirs >= wasmIndexMaxDirs {
			result.Truncated = true
			return false
		}
		visitedDirs++

		entries, err := ReadDirCompat(absDir)
		if err != nil {
			return true
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

		for _, entry := range entries {
			name := entry.Name()
			rel := name
			if relDir != "" {
				rel = relDir + "/" + name
			}

			info, err := entry.Info()
			if err != nil {
				continue
			}

			if info.IsDir() {
				if depth >= wasmIndexMaxDepth {
					continue
				}
				if wasmSkipDirs[name] {
					continue
				}
				if !walk(filepath.Join(absDir, name), rel, depth+1) {
					return false
				}
				if result.Truncated {
					return false
				}
				continue
			}

			if result.FileCount >= wasmIndexMaxFiles {
				result.Truncated = true
				return false
			}
			result.Files = append(result.Files, wasmIndexEntry{Name: name, Path: rel, Type: "file"})
			result.FileCount++
		}
		return true
	}

	walk(root, "", 0)
	result.DirCount = visitedDirs

	data, err := json.Marshal(result)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
