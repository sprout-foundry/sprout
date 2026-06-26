package filediscovery

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/index"
	"github.com/sprout-foundry/sprout/pkg/utils"
)

// Configurable constants for performance limits
const (
	// BuildWorkspaceTimeout is the absolute maximum time for workspace structure building
	BuildWorkspaceTimeout = 30 * time.Second

	// CacheDuration is how long workspace structure results are cached
	CacheDuration = 5 * time.Minute

	// ProgressEmitInterval controls how often progress events are emitted during file walk
	ProgressEmitInterval = 1000 // emit every N files

	// MaxDepth limits directory traversal depth in BuildWorkspaceStructure
	MaxDepth = 20

	// LargeWorkspaceThreshold is the file count above which limits apply
	LargeWorkspaceThreshold = 1000
)

// FileDiscovery provides common file discovery and analysis functionality
type FileDiscovery struct {
	config *configuration.Config
	logger *utils.Logger

	// cache for workspace structure results
	cacheMu  sync.RWMutex
	cacheMap map[string]*cacheEntry
}

// cacheEntry holds a cached workspace structure result
type cacheEntry struct {
	data      *WorkspaceInfo
	modTime   time.Time
	createdAt time.Time
}

// NewFileDiscovery creates a new file discovery instance
func NewFileDiscovery(cfg *configuration.Config, logger *utils.Logger) *FileDiscovery {
	return &FileDiscovery{
		config:   cfg,
		logger:   logger,
		cacheMap: make(map[string]*cacheEntry),
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
	RootDir     string
}

// DiscoverFilesRobust uses multiple strategies to find relevant files
func (fd *FileDiscovery) DiscoverFilesRobust(userIntent string, options *DiscoveryOptions) *FileResult {
	startTime := time.Now()

	if options == nil {
		options = &DiscoveryOptions{
			MaxFiles:   50,
			UseSymbols: true,
			UseShell:   true,
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
	UseSymbols    bool
	UseShell      bool
	IncludeHidden bool
	ExcludeDirs   []string
	IncludeExts   []string
	ExcludeExts   []string
	RootPath      string
}

// discoverWithShell uses shell commands to find files
func (fd *FileDiscovery) discoverWithShell(userIntent string, options *DiscoveryOptions) *FileResult {
	terms := fd.extractSearchTerms(userIntent)
	if len(terms) == 0 {
		return nil
	}

	// Build workspace info for shell commands
	workspaceInfo := &WorkspaceInfo{
		ProjectType: "other",
		RootDir:     options.RootPath,
	}
	if workspaceInfo.RootDir == "" {
		workspaceInfo.RootDir, _ = filepath.Abs(".")
	}

	joined := strings.Join(terms, " ")
	found := fd.findFilesUsingShellCommands(joined, workspaceInfo)

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

	// Check if root directory exists
	if _, err := os.Stat(root); os.IsNotExist(err) {
		return &FileResult{Error: fmt.Errorf("directory does not exist: %s", root)}
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
		for _, fs := range index.SearchSymbolFiles(idx, tokens) {
			symHits[fs.File] += len(fs.Symbols)
		}
	}

	if len(files) > 0 {
		type scoredFile struct {
			file  string
			score int
		}

		var scored []scoredFile
		for _, f := range files {
			rel, err := filepath.Rel(root, f)
			if err != nil {
				rel = f
			}
			s := symHits[filepath.ToSlash(rel)]
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

// parseQueryTerms parses a query into file patterns and search terms
func (fd *FileDiscovery) parseQueryTerms(query string) *QueryTerms {
	terms := &QueryTerms{
		FilePatterns: []string{},
		SearchTerms:  []string{},
	}

	// Split query by spaces
	parts := strings.Fields(query)

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		// Check if it's a file pattern (contains wildcards)
		if strings.Contains(part, "*") || strings.Contains(part, "?") {
			terms.FilePatterns = append(terms.FilePatterns, part)
		} else {
			// Treat as search term
			terms.SearchTerms = append(terms.SearchTerms, part)
		}
	}

	return terms
}

// QueryTerms represents parsed query terms
type QueryTerms struct {
	FilePatterns []string
	SearchTerms  []string
}

// findFilesUsingShellCommands finds files using shell commands
func (fd *FileDiscovery) findFilesUsingShellCommands(query string, workspaceInfo *WorkspaceInfo) []string {
	var found []string

	// Parse the query to extract file patterns and search terms
	terms := fd.parseQueryTerms(query)

	// Use different strategies based on the query type
	if len(terms.FilePatterns) > 0 {
		// Use find command for file pattern matching
		found = fd.findWithFindCommand(terms.FilePatterns, workspaceInfo)
	} else if len(terms.SearchTerms) > 0 {
		// Use grep command for content searching
		found = fd.findWithGrepCommand(terms.SearchTerms, workspaceInfo)
	} else {
		// Fallback to directory walk with basic filtering
		found = fd.findWithDirectoryWalk(query, workspaceInfo)
	}

	// Remove duplicates and apply security filtering
	return fd.deduplicateAndFilter(found, workspaceInfo)
}

// findWithFindCommand uses the find command to locate files by pattern
func (fd *FileDiscovery) findWithFindCommand(patterns []string, workspaceInfo *WorkspaceInfo) []string {
	var found []string

	for _, pattern := range patterns {
		// Build find command
		cmd := exec.Command("find", workspaceInfo.RootDir,
			"-type", "f", // Only files
			"-name", pattern, // Match pattern
			"-not", "-path", "*/.*", // Exclude hidden files
			"-not", "-path", "*/node_modules/*",
			"-not", "-path", "*/vendor/*",
			"-not", "-path", "*/target/*",
			"-not", "-path", "*/build/*",
			"-not", "-path", "*/dist/*")

		output, err := cmd.Output()
		if err != nil {
			// Log error but continue with other patterns
			continue
		}

		// Parse output
		lines := strings.Split(strings.TrimSpace(string(output)), "\n")
		for _, line := range lines {
			if line != "" {
				found = append(found, line)
			}
		}
	}

	return found
}

// findWithGrepCommand uses grep to search file contents
func (fd *FileDiscovery) findWithGrepCommand(searchTerms []string, workspaceInfo *WorkspaceInfo) []string {
	var found []string

	for _, term := range searchTerms {
		// Build grep command
		// Note: -l lists filenames only, -H includes filename headers
		cmd := exec.Command("grep", "-r", // Recursive
			"--include=*.go", "--include=*.js", "--include=*.ts", "--include=*.py",
			"--include=*.java", "--include=*.cpp", "--include=*.c", "--include=*.rs",
			"--include=*.php", "--include=*.rb", "--include=*.swift", "--include=*.kt",
			"--include=*.html", "--include=*.css", "--include=*.json", "--include=*.xml",
			"--include=*.yaml", "--include=*.yml", "--include=*.md", "--include=*.txt",
			"-l", // Only list filenames
			term, workspaceInfo.RootDir)

		output, err := cmd.Output()
		if err != nil {
			// Log error but continue with other terms
			continue
		}

		// Parse output (format: filename per line when using -l)
		lines := strings.Split(strings.TrimSpace(string(output)), "\n")
		for _, line := range lines {
			if line != "" {
				found = append(found, line)
			}
		}
	}

	return found
}

// findWithDirectoryWalk falls back to directory walking
func (fd *FileDiscovery) findWithDirectoryWalk(query string, workspaceInfo *WorkspaceInfo) []string {
	var found []string

	// Simple directory walk as fallback
	err := filepath.Walk(workspaceInfo.RootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip files we can't access
		}

		// Skip directories and hidden files
		if info.IsDir() || strings.HasPrefix(info.Name(), ".") {
			return nil
		}

		// Skip common non-source directories
		if strings.Contains(path, "node_modules") ||
			strings.Contains(path, "vendor") ||
			strings.Contains(path, "target") ||
			strings.Contains(path, "build") ||
			strings.Contains(path, "dist") {
			return nil
		}

		// Basic filename matching
		if strings.Contains(strings.ToLower(info.Name()), strings.ToLower(query)) {
			found = append(found, path)
		}

		return nil
	})

	if err != nil {
		// Log error but return what we found
	}

	return found
}

// deduplicateAndFilter removes duplicates and applies security filtering
func (fd *FileDiscovery) deduplicateAndFilter(files []string, workspaceInfo *WorkspaceInfo) []string {
	seen := make(map[string]bool)
	var result []string

	for _, file := range files {
		// Convert to absolute path
		absPath, err := filepath.Abs(file)
		if err != nil {
			continue
		}

		// Skip if already seen
		if seen[absPath] {
			continue
		}
		seen[absPath] = true

		// Security: ensure file is within workspace
		if !strings.HasPrefix(absPath, workspaceInfo.RootDir) {
			continue
		}

		// Check if file exists and is readable
		if info, err := os.Stat(absPath); err != nil || info.IsDir() {
			continue
		}

		result = append(result, absPath)
	}

	return result
}

// findFilesUsingShellCommandsFallback finds files using shell commands (fallback method)
func (fd *FileDiscovery) findFilesUsingShellCommandsFallback(query string, options *DiscoveryOptions) []string {
	found := []string{}

	// Use find command as fallback
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
