// Package filediscovery: file filtering and statistics (split from filediscovery.go)

package filediscovery

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// applyFiltersAndLimits applies filtering and limits to the file list
func (fd *FileDiscovery) applyFiltersAndLimits(files []string, options *DiscoveryOptions) *FileResult {
	result := make([]string, 0, len(files))

	for _, file := range files {
		// Apply exclusion filters
		shouldExclude := false
		for _, excludeDir := range options.ExcludeDirs {
			if strings.Contains(file, excludeDir) {
				shouldExclude = true
				break
			}
		}

		if shouldExclude {
			continue
		}

		result = append(result, file)

		// Apply max files limit
		if options.MaxFiles > 0 && len(result) >= options.MaxFiles {
			break
		}
	}

	return &FileResult{
		Files:        result,
		MatchedFiles: len(result),
	}
}

// GetFileStats returns statistics about files
func (fd *FileDiscovery) GetFileStats(files []string) map[string]interface{} {
	stats := map[string]interface{}{
		"total":        len(files),
		"by_extension": make(map[string]int),
		"by_directory": make(map[string]int),
		"largest_file": "",
		"max_size":     int64(0),
	}

	for _, file := range files {
		// Count by extension
		ext := filepath.Ext(file)
		if ext == "" {
			ext = "no_extension"
		}
		stats["by_extension"].(map[string]int)[ext]++

		// Count by directory
		dir := filepath.Dir(file)
		stats["by_directory"].(map[string]int)[dir]++

		// Track largest file
		if info, err := os.Stat(file); err == nil {
			if info.Size() > stats["max_size"].(int64) {
				stats["max_size"] = info.Size()
				stats["largest_file"] = file
			}
		}
	}

	return stats
}

// FilterFiles filters files based on criteria
func (fd *FileDiscovery) FilterFiles(files []string, criteria *FileFilterCriteria) []string {
	if criteria == nil {
		return files
	}

	var result []string

	for _, file := range files {
		if fd.matchesCriteria(file, criteria) {
			result = append(result, file)
		}
	}

	return result
}

// FileFilterCriteria defines filtering criteria for files
type FileFilterCriteria struct {
	IncludeExtensions []string
	ExcludeExtensions []string
	IncludePaths      []string
	ExcludePaths      []string
	MinSize           int64
	MaxSize           int64
	ModifiedAfter     time.Time
	ModifiedBefore    time.Time
}

// matchesCriteria checks if a file matches the filter criteria
func (fd *FileDiscovery) matchesCriteria(file string, criteria *FileFilterCriteria) bool {
	// Check extensions
	if len(criteria.IncludeExtensions) > 0 {
		ext := filepath.Ext(file)
		found := false
		for _, includeExt := range criteria.IncludeExtensions {
			if ext == includeExt {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	if len(criteria.ExcludeExtensions) > 0 {
		ext := filepath.Ext(file)
		for _, excludeExt := range criteria.ExcludeExtensions {
			if ext == excludeExt {
				return false
			}
		}
	}

	// Check paths
	if len(criteria.IncludePaths) > 0 {
		found := false
		for _, includePath := range criteria.IncludePaths {
			if strings.Contains(file, includePath) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	if len(criteria.ExcludePaths) > 0 {
		for _, excludePath := range criteria.ExcludePaths {
			if strings.Contains(file, excludePath) {
				return false
			}
		}
	}

	// Check file size and modification time in a single stat call
	needStat := criteria.MinSize > 0 || criteria.MaxSize > 0 ||
		!criteria.ModifiedAfter.IsZero() || !criteria.ModifiedBefore.IsZero()

	if needStat {
		if info, err := os.Stat(file); err == nil {
			if criteria.MinSize > 0 && info.Size() < criteria.MinSize {
				return false
			}
			if criteria.MaxSize > 0 && info.Size() > criteria.MaxSize {
				return false
			}

			modTime := info.ModTime()
			if !criteria.ModifiedAfter.IsZero() && modTime.Before(criteria.ModifiedAfter) {
				return false
			}
			if !criteria.ModifiedBefore.IsZero() && modTime.After(criteria.ModifiedBefore) {
				return false
			}
		}
	}

	return true
}
