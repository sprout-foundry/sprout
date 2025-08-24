package filediscovery

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/index"
	"github.com/alantheprice/ledit/pkg/utils"
	"github.com/alantheprice/ledit/pkg/workspaceinfo"
)

// FileDiscovery provides common file discovery and analysis functionality
type FileDiscovery struct {
	config *config.Config
	logger *utils.Logger
}

// NewFileDiscovery creates a new file discovery instance
func NewFileDiscovery(cfg *config.Config, logger *utils.Logger) *FileDiscovery {
	return &FileDiscovery{
		config: cfg,
		logger: logger,
	}
}

// FileResult represents the result of a file discovery operation
type FileResult struct {
	Files        []string
	Error        error
	Duration     time.Duration
	Method       string
	TotalFiles   int
	MatchedFiles int
}

// WorkspaceInfo represents workspace information
type WorkspaceInfo struct {
	ProjectType string
	AllFiles    []string
	FilesByDir  map[string][]string
	Error       error
}

// DiscoverFilesRobust uses multiple strategies to find relevant files
func (fd *FileDiscovery) DiscoverFilesRobust(userIntent string, options *DiscoveryOptions) *FileResult {
	startTime := time.Now()

	if options == nil {
		options = &DiscoveryOptions{
			MaxFiles:      50,
			UseEmbeddings: true,
			UseSymbols:    true,
			UseShell:      true,
		}
	}

	// Try embeddings first if enabled
	if options.UseEmbeddings {
		if result := fd.discoverWithEmbeddings(userIntent, options); result != nil && len(result.Files) > 0 {
			result.Duration = time.Since(startTime)
			result.Method = "embeddings"
			return result
		}
	}

	// Fallback to shell-based discovery
	if options.UseShell {
		if result := fd.discoverWithShell(userIntent, options); result != nil && len(result.Files) > 0 {
			result.Duration = time.Since(startTime)
			result.Method = "shell"
			return result
		}
	}

	// Final fallback to basic file listing
	result := fd.discoverBasic(options)
	result.Duration = time.Since(startTime)
	result.Method = "basic"

	return result
}

// DiscoveryOptions configures file discovery behavior
type DiscoveryOptions struct {
	MaxFiles      int
	UseEmbeddings bool
	UseSymbols    bool
	UseShell      bool
	IncludeHidden bool
	ExcludeDirs   []string
	IncludeExts   []string
	ExcludeExts   []string
	RootPath      string
}

// discoverWithEmbeddings uses embeddings to find relevant files
func (fd *FileDiscovery) discoverWithEmbeddings(userIntent string, options *DiscoveryOptions) *FileResult {
	workspaceFile, err := workspaceinfo.LoadWorkspaceFile()
	if err != nil {
		if !os.IsNotExist(err) {
			return &FileResult{Error: fmt.Errorf("failed to load workspace: %w", err)}
		}
		// No workspace file, skip embeddings
		return nil
	}

	fullFiles, _, err := workspaceinfo.GetFilesForContextUsingEmbeddings(userIntent, workspaceFile, fd.config, fd.logger)
	if err != nil {
		return &FileResult{Error: fmt.Errorf("embedding search failed: %w", err)}
	}

	// Rerank using symbol index if enabled
	if options.UseSymbols {
		fullFiles = fd.rerankWithSymbols(fullFiles, userIntent)
	}

	// Apply limits and filters
	result := fd.applyFiltersAndLimits(fullFiles, options)
	result.TotalFiles = len(fullFiles)

	return result
}

// discoverWithShell uses shell commands to find files
func (fd *FileDiscovery) discoverWithShell(userIntent string, options *DiscoveryOptions) *FileResult {
	terms := fd.extractSearchTerms(userIntent)
	if len(terms) == 0 {
		return nil
	}

	joined := strings.Join(terms, " ")
	found := fd.findFilesUsingShellCommands(joined, &WorkspaceInfo{ProjectType: "other"})

	result := fd.applyFiltersAndLimits(found, options)
	result.TotalFiles = len(found)

	return result
}

// discoverBasic provides basic file listing
func (fd *FileDiscovery) discoverBasic(options *DiscoveryOptions) *FileResult {
	root := options.RootPath
	if root == "" {
		root = "."
	}

	var files []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip errors
		}

		if info.IsDir() {
			// Skip excluded directories
			for _, exclude := range options.ExcludeDirs {
				if strings.Contains(path, exclude) {
					return filepath.SkipDir
				}
			}
			return nil
		}

		// Skip hidden files unless requested
		if !options.IncludeHidden && strings.HasPrefix(info.Name(), ".") {
			return nil
		}

		// Check file extensions
		if len(options.IncludeExts) > 0 {
			ext := filepath.Ext(path)
			found := false
			for _, includeExt := range options.IncludeExts {
				if ext == includeExt {
					found = true
					break
				}
			}
			if !found {
				return nil
			}
		}

		// Exclude file extensions
		if len(options.ExcludeExts) > 0 {
			ext := filepath.Ext(path)
			for _, excludeExt := range options.ExcludeExts {
				if ext == excludeExt {
					return nil
				}
			}
		}

		files = append(files, path)
		return nil
	})

	if err != nil {
		return &FileResult{Error: fmt.Errorf("failed to walk directory: %w", err)}
	}

	result := fd.applyFiltersAndLimits(files, options)
	result.TotalFiles = len(files)

	return result
}

// rerankWithSymbols reranks files based on symbol index overlap
func (fd *FileDiscovery) rerankWithSymbols(files []string, userIntent string) []string {
	root, _ := os.Getwd()
	symHits := map[string]int{}

	if idx, err := index.BuildSymbols(root); err == nil && idx != nil {
		tokens := strings.Fields(userIntent)
		for _, f := range index.SearchSymbols(idx, tokens) {
			symHits[filepath.ToSlash(f)]++
		}
	}

	if len(files) > 0 {
		type scoredFile struct {
			file  string
			score int
		}

		var scored []scoredFile
		for _, f := range files {
			s := symHits[filepath.ToSlash(f)]
			scored = append(scored, scoredFile{file: f, score: s})
		}

		// Sort by score descending, then by filename ascending
		sort.Slice(scored, func(i, j int) bool {
			if scored[i].score == scored[j].score {
				return scored[i].file < scored[j].file
			}
			return scored[i].score > scored[j].score
		})

		result := make([]string, len(scored))
		for i, s := range scored {
			result[i] = s.file
		}

		return result
	}

	return files
}

// extractSearchTerms extracts meaningful search terms from user intent
func (fd *FileDiscovery) extractSearchTerms(userIntent string) []string {
	words := strings.Fields(strings.ToLower(userIntent))
	var terms []string

	// Common words to skip
	skipWords := map[string]bool{
		"the": true, "a": true, "an": true, "and": true, "or": true,
		"but": true, "in": true, "on": true, "at": true, "to": true,
		"for": true, "of": true, "with": true, "by": true, "is": true,
		"are": true, "was": true, "were": true, "be": true, "been": true,
		"have": true, "has": true, "had": true, "do": true, "does": true,
		"did": true, "will": true, "would": true, "could": true, "should": true,
		"may": true, "might": true, "must": true, "can": true, "i": true,
		"you": true, "he": true, "she": true, "it": true, "we": true, "they": true,
		"this": true, "that": true, "these": true, "those": true,
		"find": true, "search": true, "grep": true, "look": true, "locate": true,
	}

	for _, word := range words {
		word = strings.Trim(word, ".,!?;:\"'()[]{}")
		if len(word) > 2 && !skipWords[word] {
			terms = append(terms, word)
		}
	}

	return terms
}

// findFilesUsingShellCommands finds files using shell commands (placeholder implementation)
func (fd *FileDiscovery) findFilesUsingShellCommands(query string, workspaceInfo *WorkspaceInfo) []string {
	// This is a simplified implementation
	// In a real implementation, you'd use shell commands like find, grep, etc.
	var found []string

	// Simple directory walk as fallback
	root := "."
	if err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			// Simple string matching
			if strings.Contains(strings.ToLower(path), strings.ToLower(query)) {
				found = append(found, path)
			}
		}
		return nil
	}); err != nil {
		if fd.logger != nil {
			fd.logger.LogError(utils.WrapError(err, "shell command file discovery failed"))
		}
	}

	return found
}

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

// BuildWorkspaceStructure builds workspace structure information
func (fd *FileDiscovery) BuildWorkspaceStructure() *WorkspaceInfo {
	startTime := time.Now()

	// Get all files
	var allFiles []string
	filesByDir := make(map[string][]string)

	root := "."
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip errors
		}

		if !info.IsDir() {
			allFiles = append(allFiles, path)

			dir := filepath.Dir(path)
			filesByDir[dir] = append(filesByDir[dir], path)
		}

		return nil
	})

	// Determine project type
	projectType := fd.detectProjectType(allFiles)

	duration := time.Since(startTime)
	if fd.logger != nil {
		fd.logger.Logf("Workspace analysis completed in %v: %d files, %d directories",
			duration, len(allFiles), len(filesByDir))
	}

	return &WorkspaceInfo{
		ProjectType: projectType,
		AllFiles:    allFiles,
		FilesByDir:  filesByDir,
		Error:       err,
	}
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

// GetFileStats returns statistics about files
func (fd *FileDiscovery) GetFileStats(files []string) map[string]interface{} {
	stats := map[string]interface{}{
		"total":        len(files),
		"by_extension": make(map[string]int),
		"by_directory": make(map[string]int),
		"largest_file": "",
		"max_size":     0,
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

	// Check file size
	if criteria.MinSize > 0 || criteria.MaxSize > 0 {
		if info, err := os.Stat(file); err == nil {
			if criteria.MinSize > 0 && info.Size() < criteria.MinSize {
				return false
			}
			if criteria.MaxSize > 0 && info.Size() > criteria.MaxSize {
				return false
			}
		}
	}

	// Check modification time
	if !criteria.ModifiedAfter.IsZero() || !criteria.ModifiedBefore.IsZero() {
		if info, err := os.Stat(file); err == nil {
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
