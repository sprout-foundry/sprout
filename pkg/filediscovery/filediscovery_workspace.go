// Package filediscovery: workspace structure building (split from filediscovery.go)

package filediscovery

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// BuildWorkspaceStructure builds workspace structure information.
// It applies a hard timeout (BuildWorkspaceTimeout), respects directory depth limits,
// emits progress events for large workspaces, and uses an in-memory cache.
func (fd *FileDiscovery) BuildWorkspaceStructure() *WorkspaceInfo {
	startTime := time.Now()

	root := "."
	absRoot, errAbs := filepath.Abs(root)
	if errAbs != nil {
		return &WorkspaceInfo{
			Error:   fmt.Errorf("failed to resolve root path: %w", errAbs),
			RootDir: ".",
		}
	}

	// Check cache before doing any work
	if cached := fd.getCachedWorkspace(absRoot); cached != nil {
		if fd.logger != nil {
			fd.logger.Logf("Workspace structure served from cache: %d files", len(cached.AllFiles))
		}
		return cached
	}

	// Create context with hard timeout
	ctx, cancel := context.WithTimeout(context.Background(), BuildWorkspaceTimeout)
	defer cancel()

	var allFiles []string
	filesByDir := make(map[string][]string)
	fileCount := 0

	// Calculate depth of root for relative depth tracking
	rootRel, _ := filepath.Rel(root, root)
	rootDepth := strings.Count(rootRel, string(filepath.Separator))

	// Context channel for periodic timeout checks
	done := ctx.Done()

	// Walk with timeout, depth limit, and progress tracking
	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		// Check context deadline on every call
		select {
		case <-done:
			if fd.logger != nil {
				fd.logger.Logf("BuildWorkspaceStructure timed out after %v (walked %d files)", time.Since(startTime), fileCount)
			}
			return context.DeadlineExceeded
		default:
		}

		if walkErr != nil {
			return nil // Skip errors
		}

		// Enforce max depth
		relPath, _ := filepath.Rel(root, path)
		depth := strings.Count(relPath, string(filepath.Separator)) + rootDepth
		if depth > MaxDepth {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if !info.IsDir() {
			allFiles = append(allFiles, path)
			dir := filepath.Dir(path)
			filesByDir[dir] = append(filesByDir[dir], path)
			fileCount++

			// For large workspaces, log progress periodically
			if fileCount%ProgressEmitInterval == 0 && fd.logger != nil {
				fd.logger.Logf("BuildWorkspaceStructure progress: %d files so far...", fileCount)
			}
		}

		return nil
	})

	duration := time.Since(startTime)

	// If timeout occurred, still return what we have but note the error
	if err == context.DeadlineExceeded {
		if fd.logger != nil {
			fd.logger.Logf("BuildWorkspaceStructure hit hard timeout after %v: %d files collected",
				duration, fileCount)
		}
	}

	// Determine project type
	projectType := fd.detectProjectType(allFiles)

	if fd.logger != nil {
		fd.logger.Logf("Workspace analysis completed in %v: %d files, %d directories%s",
			duration, len(allFiles), len(filesByDir),
			map[bool]string{true: " (timed out)"}[err == context.DeadlineExceeded],
		)
	}

	// Build the final result
	wi := &WorkspaceInfo{
		ProjectType: projectType,
		AllFiles:    allFiles,
		FilesByDir:  filesByDir,
		Error:       err,
		RootDir:     absRoot,
	}

	// Cache the actual result (we passed a placeholder above; cache now)
	if err == nil && fileCount > 0 {
		fd.cacheWorkspaceResult(absRoot, wi)
	}

	return wi
}

// getCachedWorkspace returns a cached workspace structure if available and not expired.
func (fd *FileDiscovery) getCachedWorkspace(rootDir string) *WorkspaceInfo {
	if fd.cacheMap == nil {
		return nil
	}
	fd.cacheMu.RLock()
	defer fd.cacheMu.RUnlock()

	entry, ok := fd.cacheMap[rootDir]
	if !ok {
		return nil
	}

	// Check if cache is still valid (not expired and root directory hasn't changed)
	if time.Since(entry.createdAt) > CacheDuration {
		return nil
	}

	// Check if root directory modification time has changed (indicating changes)
	if currentMod, err := fd.getDirModTime(rootDir); err == nil && currentMod != entry.modTime {
		return nil
	}

	return entry.data
}

// cacheWorkspaceResult stores a workspace structure result in the cache.
func (fd *FileDiscovery) cacheWorkspaceResult(rootDir string, data *WorkspaceInfo) {
	if fd.cacheMap == nil {
		return
	}
	fd.cacheMu.Lock()
	defer fd.cacheMu.Unlock()

	if modTime, err := fd.getDirModTime(rootDir); err == nil {
		fd.cacheMap[rootDir] = &cacheEntry{
			data:      data,
			modTime:   modTime,
			createdAt: time.Now(),
		}
		// Clean up expired entries while we have the write lock
		now := time.Now()
		for k, v := range fd.cacheMap {
			if k != rootDir && now.Sub(v.createdAt) > CacheDuration {
				delete(fd.cacheMap, k)
			}
		}
	}
}

// getDirModTime returns the most recent modification time of immediate children of rootDir.
// This is used to detect if the workspace has changed since it was last cached.
func (fd *FileDiscovery) getDirModTime(rootDir string) (time.Time, error) {
	entries, err := os.ReadDir(rootDir)
	if err != nil {
		return time.Time{}, err
	}
	var latest time.Time
	for _, e := range entries {
		info, err := e.Info()
		if err == nil && info.ModTime().After(latest) {
			latest = info.ModTime()
		}
	}
	return latest, nil
}

// detectProjectType attempts to detect the project type
func (fd *FileDiscovery) detectProjectType(files []string) string {
	// Check for common project markers
	projectMarkers := map[string]string{
		"go.mod":           "go",
		"package.json":     "nodejs",
		"requirements.txt": "python",
		"pyproject.toml":   "python",
		"Cargo.toml":       "rust",
		"pom.xml":          "java",
		"build.gradle":     "java",
		"CMakeLists.txt":   "c/c++",
		"Makefile":         "c/c++/make",
		".git":             "git",
		"README.md":        "documentation",
	}

	for _, file := range files {
		filename := filepath.Base(file)
		if projectType, exists := projectMarkers[filename]; exists {
			return projectType
		}
	}

	return "unknown"
}
