package webui

import (
	"os"
	"path/filepath"
	"strings"
)

// ProjectMarker represents a file or directory that indicates a project root.
type ProjectMarker struct {
	Name   string // e.g., ".git", "go.mod"
	Weight int    // Higher = stronger signal
	IsDir  bool
}

// projectMarkers defines the markers we look for, ordered by weight (highest first).
var projectMarkers = []ProjectMarker{
	{".git", 100, true},
	{".sprout", 90, true},
	{"go.mod", 80, false},
	{"package.json", 80, false},
	{"Cargo.toml", 80, false},
	{"pyproject.toml", 80, false},
	{"setup.py", 70, false},
	{"CMakeLists.txt", 70, false},
	{"Gemfile", 70, false},
	{"requirements.txt", 60, false},
	{"Makefile", 50, false},
	{"justfile", 50, false},
	{".vscode", 40, true},
	{"README.md", 30, false},
}

// ProjectInfo describes a detected project directory.
type ProjectInfo struct {
	Path    string   `json:"path"`
	Name    string   `json:"name"`
	Markers []string `json:"markers,omitempty"`
}

// IsProjectDirectory checks if a directory appears to be a project root.
// Returns (isProject, markersFound). A directory is a project if it contains
// at least one marker with weight >= 50, or two+ markers with weight >= 30.
func IsProjectDirectory(dir string) (bool, []string) {
	dir = filepath.Clean(dir)
	markers := findMarkersInDir(dir)
	if len(markers) == 0 {
		return false, nil
	}

	// Check if any marker has weight >= 50
	for _, marker := range markers {
		for _, pm := range projectMarkers {
			if pm.Name == marker && pm.Weight >= 50 {
				return true, markers
			}
		}
	}

	// Check if 2+ markers with weight >= 30
	count := 0
	for _, marker := range markers {
		for _, pm := range projectMarkers {
			if pm.Name == marker && pm.Weight >= 30 {
				count++
				break
			}
		}
	}
	if count >= 2 {
		return true, markers
	}

	return false, markers
}

// findMarkersInDir returns the list of project markers found in a directory.
// It does NOT follow symlinks; it only checks if an entry with the given name
// exists in the directory listing.
func findMarkersInDir(dir string) []string {
	var found []string
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	for _, entry := range entries {
		name := entry.Name()
		for _, pm := range projectMarkers {
			if pm.Name == name {
				if pm.IsDir && !entry.IsDir() {
					continue
				}
				if !pm.IsDir && entry.IsDir() {
					continue
				}
				found = append(found, name)
				break
			}
		}
	}
	return found
}

// FindNearestProjectRoot walks up from startDir looking for project markers.
// Returns (projectRoot, markers) or ("", nil) if none found.
// Stops at the filesystem root.
func FindNearestProjectRoot(startDir string) (string, []string) {
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return "", nil
	}
	dir = filepath.Clean(dir)
	for {
		if isProject, markers := IsProjectDirectory(dir); isProject {
			return dir, markers
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break // reached root
		}
		dir = parent
	}
	return "", nil
}

// FindProjectsInDirectory scans a directory for subdirectories that look like
// projects, up to maxDepth levels deep. Returns at most 20 results.
func FindProjectsInDirectory(dir string, maxDepth int) []ProjectInfo {
	dir = filepath.Clean(dir)
	var results []ProjectInfo
	if maxDepth <= 0 {
		maxDepth = 2
	}
	walkForProjects(dir, maxDepth, 0, &results)
	if len(results) > 20 {
		results = results[:20]
	}
	return results
}

func walkForProjects(dir string, maxDepth, depth int, results *[]ProjectInfo) {
	if depth > maxDepth || len(*results) >= 20 {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		subDir := filepath.Join(dir, entry.Name())
		if isProject, _ := IsProjectDirectory(subDir); isProject {
			*results = append(*results, ProjectInfo{
				Path: subDir,
				Name: entry.Name(),
			})
		} else if depth < maxDepth {
			walkForProjects(subDir, maxDepth, depth+1, results)
		}
	}
}

// isProjectRoot is a simple wrapper around IsProjectDirectory for use in
// server startup code that doesn't need the markers return value.
func isProjectRoot(dir string) bool {
	isProject, _ := IsProjectDirectory(dir)
	return isProject
}
